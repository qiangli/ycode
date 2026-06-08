package api

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// ModelInfo represents a single available model across all sources.
type ModelInfo struct {
	ID       string `json:"id"`                // full model ID (e.g. "claude-opus-4-6-20250415", "llama3.2:3b")
	Alias    string `json:"alias,omitempty"`   // short alias if any (e.g. "opus")
	Provider string `json:"provider"`          // provider name (e.g. "anthropic", "ollama")
	Source   string `json:"source"`            // "builtin", "config", "env", "ollama", "cloudbox"
	Size     string `json:"size,omitempty"`    // human-readable size (Ollama models only)
	Current  bool   `json:"current,omitempty"` // true if this is the active model
}

// OllamaLister is a callback that returns locally available Ollama models.
// Implementations should use a short timeout and return nil on failure.
type OllamaLister func(ctx context.Context) []ModelInfo

// DetectProviderFromModel guesses provider from a model name prefix.
func DetectProviderFromModel(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lower, "claude-"):
		return "anthropic"
	case strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1-") || strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4"):
		return "openai"
	case strings.HasPrefix(lower, "gemini-"):
		return "gemini"
	case strings.HasPrefix(lower, "grok"):
		return "xai"
	case strings.HasPrefix(lower, "qwen"):
		return "dashscope"
	case strings.HasPrefix(lower, "kimi") || strings.HasPrefix(lower, "moonshot"):
		return "moonshot"
	default:
		return "unknown"
	}
}

// envKeyModels maps environment variable names to their well-known flagship models.
var envKeyModels = []struct {
	envKey   string
	provider string
	models   []string
}{
	{"ANTHROPIC_API_KEY", "anthropic", []string{
		"claude-opus-4-6-20250415",
		"claude-sonnet-4-6-20250514",
		"claude-haiku-4-5-20251001",
	}},
	{"OPENAI_API_KEY", "openai", []string{
		"gpt-4.1",
		"gpt-4.1-mini",
		"gpt-4.1-nano",
		"o3",
		"o4-mini",
	}},
	{"GOOGLE_API_KEY", "gemini", []string{
		"gemini-2.5-pro",
		"gemini-2.5-flash",
	}},
	{"GEMINI_API_KEY", "gemini", []string{
		"gemini-2.5-pro",
		"gemini-2.5-flash",
	}},
	{"XAI_API_KEY", "xai", []string{
		"grok-3",
		"grok-3-mini",
	}},
	{"DASHSCOPE_API_KEY", "dashscope", []string{
		"qwen-max",
		"qwen-plus",
		"qwen-turbo",
	}},
	{"MOONSHOT_API_KEY", "moonshot", []string{
		"kimi-k2.5",
		"moonshot-v1-128k",
	}},
	{"KIMI_API_KEY", "moonshot", []string{
		"kimi-k2.5",
	}},
}

// DiscoverModels aggregates all available models from five sources:
//  1. Built-in aliases (hardcoded defaults)
//  2. Config file aliases (user-defined in settings.json)
//  3. Env-detected models (API keys present in environment)
//  4. Ollama local models (dynamically queried via ollamaLister callback)
//  5. Cloudbox-pooled models (dynamically queried via cloudboxLister callback)
//
// The configAliases parameter should be config.Aliases (may be nil).
// The ollamaLister and cloudboxLister parameters are optional; pass nil to
// skip the respective source.
func DiscoverModels(ctx context.Context, configAliases map[string]string, ollamaLister OllamaLister, cloudboxLister CloudboxLister) []ModelInfo {
	seen := make(map[string]bool) // track model IDs to avoid duplicates
	var models []ModelInfo

	// 1. Built-in aliases.
	for alias, fullID := range ModelAliases {
		models = append(models, ModelInfo{
			ID:       fullID,
			Alias:    alias,
			Provider: DetectProviderFromModel(fullID),
			Source:   "builtin",
		})
		seen[fullID] = true
	}

	// 2. Config file aliases.
	for alias, target := range configAliases {
		// Resolve through built-in aliases (e.g. "fast" → "haiku" → full ID).
		fullID := ResolveModel(target)
		if seen[fullID] {
			continue
		}
		models = append(models, ModelInfo{
			ID:       fullID,
			Alias:    alias,
			Provider: DetectProviderFromModel(fullID),
			Source:   "config",
		})
		seen[fullID] = true
	}

	// 3. Env-detected models.
	models = appendEnvModels(models, seen)

	// 4. Ollama local models.
	if ollamaLister != nil {
		for _, om := range ollamaLister(ctx) {
			if seen[om.ID] {
				continue
			}
			models = append(models, om)
			seen[om.ID] = true
		}
	}

	// 5. Cloudbox-pooled models.
	if cloudboxLister != nil {
		for _, cm := range cloudboxLister(ctx) {
			if seen[cm.ID] {
				continue
			}
			models = append(models, cm)
			seen[cm.ID] = true
		}
	}

	return models
}

// DiscoverCloudboxOnly returns only the cloudbox-pooled models. Used by
// `ycode serve`'s /api/models endpoint where cloudbox is the sole source.
// Returns an empty slice (not nil) when the lister is nil or returns nothing.
func DiscoverCloudboxOnly(ctx context.Context, cloudboxLister CloudboxLister) []ModelInfo {
	if cloudboxLister == nil {
		return []ModelInfo{}
	}
	models := cloudboxLister(ctx)
	if models == nil {
		return []ModelInfo{}
	}
	return models
}

// DiscoverEnvAndCloudbox returns env-detected flagship models merged with
// cloudbox-pooled models, deduped by ID. Used by the TUI /model picker:
// env (local) + cloudbox, intentionally excluding built-in aliases, config
// aliases, and the local Ollama daemon.
func DiscoverEnvAndCloudbox(ctx context.Context, cloudboxLister CloudboxLister) []ModelInfo {
	seen := make(map[string]bool)
	var models []ModelInfo

	models = appendEnvModels(models, seen)

	if cloudboxLister != nil {
		for _, cm := range cloudboxLister(ctx) {
			if seen[cm.ID] {
				continue
			}
			models = append(models, cm)
			seen[cm.ID] = true
		}
	}

	return models
}

// appendEnvModels walks envKeyModels and appends any flagship models whose
// API key is set in the environment, deduping via the shared `seen` map.
// It returns the (possibly extended) slice.
func appendEnvModels(models []ModelInfo, seen map[string]bool) []ModelInfo {
	envSeen := make(map[string]bool) // avoid duplicate providers (GOOGLE_API_KEY and GEMINI_API_KEY)
	for _, entry := range envKeyModels {
		if os.Getenv(entry.envKey) == "" {
			continue
		}
		if envSeen[entry.provider] {
			continue
		}
		envSeen[entry.provider] = true
		for _, modelID := range entry.models {
			if seen[modelID] {
				continue
			}
			models = append(models, ModelInfo{
				ID:       modelID,
				Provider: entry.provider,
				Source:   "env",
			})
			seen[modelID] = true
		}
	}
	return models
}

// FormatBytes formats a byte count into a human-readable string.
func FormatBytes(b int64) string {
	const (
		gb = 1 << 30
		mb = 1 << 20
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.0f MB", float64(b)/float64(mb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

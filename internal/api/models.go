package api

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// ModelInfo represents a single available model across all sources.
type ModelInfo struct {
	ID       string `json:"id"`                // full model ID (e.g. "claude-opus-4-8", "llama3.2:3b")
	Alias    string `json:"alias,omitempty"`   // short alias if any (e.g. "opus")
	Provider string `json:"provider"`          // provider name (e.g. "anthropic", "openai")
	Source   string `json:"source"`            // "builtin", "config", "env", "cloudbox"
	Size     string `json:"size,omitempty"`    // human-readable size when a provider reports it
	Current  bool   `json:"current,omitempty"` // true if this is the active model
}

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
		return "kimi"
	case strings.HasPrefix(lower, "deepseek"):
		return "deepseek"
	case strings.HasPrefix(lower, "glm"):
		return "glm"
	default:
		return "unknown"
	}
}

// providerEnvKey returns the conventional API-key env var(s) for a provider
// name as returned by DetectProviderFromModel. Empty for "unknown".
func providerEnvKey(provider string) string {
	switch provider {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "gemini":
		return "GOOGLE_API_KEY (or GEMINI_API_KEY)"
	case "xai":
		return "XAI_API_KEY"
	case "dashscope":
		return "DASHSCOPE_API_KEY"
	case "kimi", "moonshot":
		return "MOONSHOT_API_KEY (or KIMI_API_KEY)"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "glm", "zai":
		return "ZAI_API_KEY (or GLM_API_KEY)"
	default:
		return ""
	}
}

// envKeyModels maps environment variable names to their well-known flagship models.
var envKeyModels = []struct {
	envKey   string
	provider string
	models   []string
}{
	{"ANTHROPIC_API_KEY", "anthropic", []string{
		"claude-opus-4-8",
		"claude-sonnet-5",
		"claude-haiku-4-5",
		"claude-fable-5",
	}},
	{"OPENAI_API_KEY", "openai", []string{
		"gpt-5.5",
		"gpt-5.6",
		"gpt-5.6-terra",
		"gpt-5.6-sol",
		"gpt-5.6-luna",
		"gpt-5.4-mini",
		"o3",
		"o4-mini",
	}},
	{"GOOGLE_API_KEY", "gemini", []string{
		"gemini-3.1-pro",
		"gemini-3.5-flash",
	}},
	{"GEMINI_API_KEY", "gemini", []string{
		"gemini-3.1-pro",
		"gemini-3.5-flash",
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
	{"MOONSHOT_API_KEY", "kimi", []string{
		"kimi-k2.7-code",
		"kimi-k2.6",
	}},
	{"KIMI_API_KEY", "kimi", []string{
		"kimi-k2.7-code",
		"kimi-k2.6",
	}},
	{"DEEPSEEK_API_KEY", "deepseek", []string{
		"deepseek-v4-pro",
		"deepseek-v4-flash",
		"deepseek-chat",
	}},
	// z.ai (Zhipu) GLM. The CODING PLAN endpoint is /api/coding/paas/v4 — a different
	// path from the general /api/paas/v4, and a coding key is rejected by the latter.
	{"ZAI_API_KEY", "glm", []string{
		"glm-5.2",
	}},
	{"GLM_API_KEY", "glm", []string{
		"glm-5.2",
	}},
}

// DiscoverModels aggregates all available models from four sources:
//  1. Built-in aliases (hardcoded defaults)
//  2. Config file aliases (user-defined in settings.json)
//  3. Env-detected models (API keys present in environment)
//  4. Cloudbox-pooled models (dynamically queried via cloudboxLister callback)
//
// The configAliases parameter should be config.Aliases (may be nil).
// The cloudboxLister parameter is optional; pass nil to skip that source.
func DiscoverModels(ctx context.Context, configAliases map[string]string, cloudboxLister CloudboxLister) []ModelInfo {
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

	// 4. Cloudbox-pooled models.
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
// aliases.
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

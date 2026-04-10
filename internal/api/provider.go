package api

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/oauth"
)

// ModelAliases maps short names to full model IDs.
var ModelAliases = map[string]string{
	"opus":   "claude-opus-4-6-20250415",
	"sonnet": "claude-sonnet-4-6-20250514",
	"haiku":  "claude-haiku-4-5-20251001",
	"kimi":   "kimi-k2.5",
}

// ProviderConfig holds provider-specific settings for client creation.
type ProviderConfig struct {
	Kind        ProviderKind
	DisplayName string // human-readable name (e.g. "moonshot", "xai"); defaults to Kind
	APIKey      string
	BearerToken string
	BaseURL     string
}

// DisplayKind returns the human-readable provider name.
func (c *ProviderConfig) DisplayKind() string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	return string(c.Kind)
}

// ResolveModel expands a built-in model alias to its full model ID.
// If the model is not an alias, it is returned as-is.
func ResolveModel(model string) string {
	return ResolveModelWithAliases(model, nil)
}

// ResolveModelWithAliases resolves a model name through two stages:
// 1. User-defined config aliases (highest priority)
// 2. Built-in ModelAliases
// If a config alias resolves to a built-in alias (e.g., "fast" → "haiku" → full ID),
// the second hop happens automatically.
func ResolveModelWithAliases(model string, configAliases map[string]string) string {
	lower := strings.ToLower(model)

	// Stage 1: check user-defined aliases.
	if configAliases != nil {
		for k, v := range configAliases {
			if strings.ToLower(k) == lower {
				model = v
				lower = strings.ToLower(v)
				break
			}
		}
	}

	// Stage 2: check built-in aliases.
	if resolved, ok := ModelAliases[lower]; ok {
		return resolved
	}
	return model
}

// DetectProvider determines which provider to use based on the model name
// and available environment variables. It follows this priority:
//
//  1. Model name prefix/alias → known provider
//  2. Explicit base URL env vars (OPENAI_BASE_URL) → OpenAI-compatible
//  3. Available API keys: ANTHROPIC_API_KEY, OPENAI_API_KEY, XAI_API_KEY, DASHSCOPE_API_KEY
//  4. Default: Anthropic
func DetectProvider(model string) (*ProviderConfig, error) {
	resolved := ResolveModel(model)

	// Check model name for provider hints.
	if cfg, matched := detectFromModel(resolved); matched {
		if cfg != nil {
			return cfg, nil
		}
		return nil, fmt.Errorf("model %q requires provider credentials; see provider-specific env vars", model)
	}

	// Check for explicit base URL override (implies OpenAI-compatible).
	if baseURL := envNonEmpty("OPENAI_BASE_URL"); baseURL != "" {
		apiKey := envNonEmpty("OPENAI_API_KEY")
		// Allow empty key for local providers like Ollama.
		return &ProviderConfig{
			Kind:    ProviderOpenAI,
			APIKey:  apiKey,
			BaseURL: baseURL,
		}, nil
	}

	// Check available API keys in priority order.
	if key := envNonEmpty("ANTHROPIC_API_KEY"); key != "" {
		return &ProviderConfig{
			Kind:    ProviderAnthropic,
			APIKey:  key,
			BaseURL: envNonEmpty("ANTHROPIC_BASE_URL"),
		}, nil
	}
	if key := envNonEmpty("OPENAI_API_KEY"); key != "" {
		return &ProviderConfig{
			Kind:    ProviderOpenAI,
			APIKey:  key,
			BaseURL: "https://api.openai.com/v1",
		}, nil
	}
	if key := envNonEmpty("XAI_API_KEY"); key != "" {
		return &ProviderConfig{
			Kind:    ProviderOpenAI,
			APIKey:  key,
			BaseURL: envNonEmpty("XAI_BASE_URL", "https://api.x.ai/v1"),
		}, nil
	}
	if key := envNonEmpty("DASHSCOPE_API_KEY"); key != "" {
		return &ProviderConfig{
			Kind:    ProviderOpenAI,
			APIKey:  key,
			BaseURL: envNonEmpty("DASHSCOPE_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		}, nil
	}
	if key := envNonEmpty("MOONSHOT_API_KEY"); key != "" {
		return &ProviderConfig{
			Kind:    ProviderOpenAI,
			APIKey:  key,
			BaseURL: envNonEmpty("MOONSHOT_BASE_URL", "https://api.moonshot.ai/v1"),
		}, nil
	}
	if key := envNonEmpty("KIMI_API_KEY"); key != "" {
		return &ProviderConfig{
			Kind:    ProviderOpenAI,
			APIKey:  key,
			BaseURL: envNonEmpty("KIMI_BASE_URL", "https://api.moonshot.ai/v1"),
		}, nil
	}

	// Fall back to saved OAuth credentials for Anthropic.
	if token, err := resolveOAuthToken(); err == nil && token != "" {
		return &ProviderConfig{
			Kind:        ProviderAnthropic,
			BearerToken: token,
			BaseURL:     envNonEmpty("ANTHROPIC_BASE_URL"),
		}, nil
	}

	return nil, fmt.Errorf("no API key found; set one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, XAI_API_KEY, DASHSCOPE_API_KEY, MOONSHOT_API_KEY, KIMI_API_KEY\nor run: ycode login")
}

// NewProvider creates a Provider from a ProviderConfig.
func NewProvider(cfg *ProviderConfig) Provider {
	switch cfg.Kind {
	case ProviderAnthropic:
		var opts []AnthropicOption
		if cfg.BaseURL != "" {
			opts = append(opts, WithBaseURL(cfg.BaseURL))
		}
		if cfg.BearerToken != "" {
			opts = append(opts, WithBearerToken(cfg.BearerToken))
		}
		return NewAnthropicClient(cfg.APIKey, opts...)
	default:
		return NewOpenAICompatClient(cfg.APIKey, cfg.BaseURL)
	}
}

// detectFromModel checks whether the model name implies a specific provider.
// Returns (config, true) when the model matches a known provider pattern.
// If matched but no credentials are found, returns (nil, true) so the caller
// can report a clear error instead of silently falling through to a wrong provider.
func detectFromModel(model string) (*ProviderConfig, bool) {
	lower := strings.ToLower(model)

	switch {
	case strings.HasPrefix(lower, "claude-") || isClaudeAlias(lower):
		if key := envNonEmpty("ANTHROPIC_API_KEY"); key != "" {
			return &ProviderConfig{
				Kind:    ProviderAnthropic,
				APIKey:  key,
				BaseURL: envNonEmpty("ANTHROPIC_BASE_URL"),
			}, true
		}
		if token, err := resolveOAuthToken(); err == nil && token != "" {
			return &ProviderConfig{
				Kind:        ProviderAnthropic,
				BearerToken: token,
				BaseURL:     envNonEmpty("ANTHROPIC_BASE_URL"),
			}, true
		}
		return nil, true
	case strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1-") ||
		strings.HasPrefix(lower, "o3-") || strings.HasPrefix(lower, "openai/"):
		if key := envNonEmpty("OPENAI_API_KEY"); key != "" {
			return &ProviderConfig{
				Kind:    ProviderOpenAI,
				APIKey:  key,
				BaseURL: envNonEmpty("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			}, true
		}
		return nil, true
	case strings.HasPrefix(lower, "grok") || strings.HasPrefix(lower, "xai/"):
		if key := envNonEmpty("XAI_API_KEY"); key != "" {
			return &ProviderConfig{
				Kind:        ProviderOpenAI,
				DisplayName: "xai",
				APIKey:      key,
				BaseURL:     envNonEmpty("XAI_BASE_URL", "https://api.x.ai/v1"),
			}, true
		}
		return nil, true
	case strings.HasPrefix(lower, "qwen") || strings.HasPrefix(lower, "dashscope/"):
		if key := envNonEmpty("DASHSCOPE_API_KEY"); key != "" {
			return &ProviderConfig{
				Kind:        ProviderOpenAI,
				DisplayName: "dashscope",
				APIKey:      key,
				BaseURL:     envNonEmpty("DASHSCOPE_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
			}, true
		}
		return nil, true
	case strings.HasPrefix(lower, "moonshot") || strings.HasPrefix(lower, "kimi"):
		if key := envNonEmpty("MOONSHOT_API_KEY"); key != "" {
			return &ProviderConfig{
				Kind:        ProviderOpenAI,
				DisplayName: "moonshot",
				APIKey:      key,
				BaseURL:     envNonEmpty("MOONSHOT_BASE_URL", "https://api.moonshot.ai/v1"),
			}, true
		}
		if key := envNonEmpty("KIMI_API_KEY"); key != "" {
			return &ProviderConfig{
				Kind:        ProviderOpenAI,
				DisplayName: "moonshot",
				APIKey:      key,
				BaseURL:     envNonEmpty("KIMI_BASE_URL", "https://api.moonshot.ai/v1"),
			}, true
		}
		return nil, true
	}

	return nil, false
}

func isClaudeAlias(s string) bool {
	_, ok := ModelAliases[s]
	return ok
}

// resolveOAuthToken loads saved OAuth credentials and returns a valid access token.
// If the token is expired and a refresh token is available, it refreshes automatically.
func resolveOAuthToken() (string, error) {
	token, err := oauth.LoadCredentials()
	if err != nil {
		return "", err
	}
	if !token.IsExpired() {
		return token.AccessToken, nil
	}
	// Token expired - try to refresh.
	if token.RefreshToken == "" {
		return "", fmt.Errorf("oauth token expired and no refresh token available; run: ycode login")
	}
	refreshed, err := oauth.RefreshToken(context.Background(), oauth.DefaultTokenURL, oauth.DefaultClientID, token.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("oauth token refresh failed: %w; run: ycode login", err)
	}
	// Save the refreshed token.
	if err := oauth.SaveCredentials(refreshed); err != nil {
		// Non-fatal: we have a valid token, just couldn't persist it.
		fmt.Fprintf(os.Stderr, "warning: failed to save refreshed token: %v\n", err)
	}
	return refreshed.AccessToken, nil
}

// envNonEmpty returns the value of the first env var that is set and non-empty.
// If defaults are provided, the first one is returned when no env var matches.
func envNonEmpty(keys ...string) string {
	if len(keys) == 0 {
		return ""
	}
	// First key is always an env var name.
	val := os.Getenv(keys[0])
	if val != "" {
		return val
	}
	// Remaining keys are fallback defaults (not env vars).
	if len(keys) > 1 {
		return keys[1]
	}
	return ""
}

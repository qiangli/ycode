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
	"opus":           "claude-opus-4-8",
	"sonnet":         "claude-sonnet-5",
	"haiku":          "claude-haiku-4-5",
	"kimi":           "kimi-k2.7-code",
	"gemini-pro":     "gemini-3.1-pro",
	"gemini-flash":   "gemini-3.5-flash",
	"deepseek":       "deepseek-chat",
	"deepseek-r1":    "deepseek-reasoner",
	"deepseek-flash": "deepseek-v4-flash",
	"deepseek-pro":   "deepseek-v4-pro",
	"glm":            "glm-5.2",
}

// ProviderConfig holds provider-specific settings for client creation.
type ProviderConfig struct {
	Kind        ProviderKind
	DisplayName string // human-readable name (e.g. "moonshot", "xai"); defaults to Kind
	APIKey      string
	BearerToken string
	BaseURL     string
}

// DefaultModelForProvider returns ycode's known-good default for a configured
// provider. It is used only after the requested model is rejected as missing.
// An empty result means ycode does not know a safe replacement for that endpoint.
func DefaultModelForProvider(cfg ProviderConfig) string {
	switch strings.ToLower(cfg.DisplayName) {
	case "deepseek":
		return "deepseek-chat"
	case "zai":
		return "glm-5.2"
	case "gemini":
		return "gemini-2.5-pro"
	case "moonshot":
		return "kimi-k2.5"
	}

	switch cfg.Kind {
	case ProviderAnthropic:
		return "claude-sonnet-4-6-20250514"
	case ProviderOpenAI:
		return "gpt-4.1"
	case ProviderGemini:
		return "gemini-2.5-pro"
	default:
		return ""
	}
}

// DisplayKind returns the human-readable provider name.
func (c *ProviderConfig) DisplayKind() string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	return string(c.Kind)
}

// credential returns the configured secret material (API key, else OAuth
// bearer token) for fingerprinting. It must never be printed in full — only
// via KeyFingerprint.
func (c *ProviderConfig) credential() string {
	if c.APIKey != "" {
		return c.APIKey
	}
	return c.BearerToken
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
//  1. DHNT_BASE_URL + tagged model name (name:tag) → cloudbox-pooled
//     OpenAI-compatible, so pooled Ollama-style names are not stolen by
//     commercial provider prefixes.
//  2. Model name prefix/alias match with available credentials (e.g.
//     "kimi-k2.7-code" + KIMI_API_KEY → Moonshot) — beats DHNT so that
//     explicit per-provider API keys route directly, not through cloudbox.
//  3. DHNT_BASE_URL (+ DHNT_API_KEY) → cloudbox-pooled OpenAI-compatible
//  4. Model name match without credentials → error
//  5. OPENAI_BASE_URL (+ OPENAI_API_KEY) → generic OpenAI-compatible
//  6. Available API keys: ANTHROPIC_API_KEY, OPENAI_API_KEY, XAI_API_KEY,
//     DASHSCOPE_API_KEY, MOONSHOT_API_KEY, KIMI_API_KEY, DEEPSEEK_API_KEY,
//     GOOGLE_API_KEY, GEMINI_API_KEY
//  7. OAuth token → Anthropic
//
// The DHNT_* pair follows the existing per-provider convention
// (<PROVIDER>_BASE_URL + <PROVIDER>_API_KEY, same as OPENAI_*,
// ANTHROPIC_*, XAI_*, DASHSCOPE_*, MOONSHOT_*, KIMI_*) and takes
// precedence over OPENAI_* so a user who has OPENAI_API_KEY exported
// for direct OpenAI access from another tool can ALSO set
// DHNT_BASE_URL + DHNT_API_KEY to route ycode through cloudbox's
// pooled models (https://ai.dhnt.io/v1) without disturbing the other tool
// in the same shell. The two paths are independent — neither clobbers
// the other.
func DetectProvider(model string) (*ProviderConfig, error) {
	resolved := ResolveModel(model)

	// Pooled cloudbox/Ollama models are tagged as name:tag. Some names collide
	// with direct commercial-provider prefixes (deepseek-*, glm-*, gpt-*), so
	// route tagged models through DHNT before provider-prefix detection can
	// capture them.
	if baseURL := envNonEmpty("DHNT_BASE_URL"); baseURL != "" && strings.Contains(resolved, ":") {
		return &ProviderConfig{
			Kind:    ProviderOpenAI,
			APIKey:  envNonEmpty("DHNT_API_KEY"),
			BaseURL: baseURL,
		}, nil
	}

	// 2. Check untagged model names for provider hints with available credentials.
	// When the model matches a known provider AND the corresponding API
	// key is set, route directly — the user's explicit per-provider key
	// beats the cloudbox override for commercial cloud model IDs.
	cfg, modelMatched := detectFromModel(resolved)
	if modelMatched && cfg != nil && (cfg.APIKey != "" || cfg.BearerToken != "") {
		return cfg, nil
	}

	// 2b. Model matched a commercial provider prefix (deepseek-*, gpt-*,
	// claude-*, …) but its API key is not set. Do NOT fall through to the
	// DHNT cloudbox pool — commercial cloud model IDs would 404 with a cryptic
	// "no reachable backend has model <name>". Report a clear, actionable
	// error naming the key to set. A nil cfg here means "matched but no
	// creds".
	if modelMatched && cfg == nil {
		if envKey := providerEnvKey(DetectProviderFromModel(resolved)); envKey != "" {
			return nil, fmt.Errorf("model %q requires %s; set it (or `ycode login`), or pick a model your configured provider serves", model, envKey)
		}
		return nil, fmt.Errorf("model %q requires provider credentials; set the matching provider API key, or pick a different model", model)
	}

	// 3. DHNT cloudbox-pooled override. Checked after model-specific
	// credentials so that explicitly-set per-provider API keys remain
	// usable alongside DHNT for untagged model names.
	if baseURL := envNonEmpty("DHNT_BASE_URL"); baseURL != "" {
		// Allow empty key: cloudbox returns 401 on /v1/* without an
		// llm:chat bearer, which surfaces as a clear error at the
		// first inference call.
		return &ProviderConfig{
			Kind:    ProviderOpenAI,
			APIKey:  envNonEmpty("DHNT_API_KEY"),
			BaseURL: baseURL,
		}, nil
	}

	// 3. Model matched a known provider but no credentials found and no
	// DHNT override available.
	if modelMatched {
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
	if key := envNonEmpty("DEEPSEEK_API_KEY"); key != "" {
		return &ProviderConfig{
			Kind:        ProviderOpenAI,
			DisplayName: "deepseek",
			APIKey:      key,
			BaseURL:     envNonEmpty("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
		}, nil
	}
	if key := zaiKey(); key != "" {
		return &ProviderConfig{
			Kind:        ProviderOpenAI,
			DisplayName: "zai",
			APIKey:      key,
			BaseURL:     envNonEmpty("ZAI_BASE_URL", "https://api.z.ai/api/coding/paas/v4"),
		}, nil
	}
	if key := envNonEmpty("GOOGLE_API_KEY"); key != "" {
		return &ProviderConfig{
			Kind:        ProviderGemini,
			DisplayName: "gemini",
			APIKey:      key,
			BaseURL:     envNonEmpty("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai"),
		}, nil
	}
	if key := envNonEmpty("GEMINI_API_KEY"); key != "" {
		return &ProviderConfig{
			Kind:        ProviderGemini,
			DisplayName: "gemini",
			APIKey:      key,
			BaseURL:     envNonEmpty("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai"),
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

	return nil, fmt.Errorf("no API key found; set one of: DHNT_BASE_URL (+ DHNT_API_KEY), ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY, GEMINI_API_KEY, XAI_API_KEY, DASHSCOPE_API_KEY, MOONSHOT_API_KEY, KIMI_API_KEY, DEEPSEEK_API_KEY, ZAI_API_KEY\nor run: ycode login")
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
	case ProviderGemini:
		return NewOpenAICompatClient(cfg.APIKey, cfg.BaseURL)
	case ProviderLocal:
		// External OpenAI-compatible provider with no API key.
		return NewOpenAICompatClient("", cfg.BaseURL)
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
	case strings.HasPrefix(lower, "glm") || strings.HasPrefix(lower, "zai/"):
		// NOTE: envNonEmpty(a, b) does NOT mean "try a then b" — the second argument is
		// a literal DEFAULT VALUE. Calling envNonEmpty("ZAI_API_KEY", "GLM_API_KEY")
		// would send the literal string "GLM_API_KEY" as the bearer token whenever
		// ZAI_API_KEY was unset: a silent auth failure that reads as a broken model.
		// Look each one up properly, as moonshot/kimi do.
		if key := zaiKey(); key != "" {
			return &ProviderConfig{
				Kind:        ProviderOpenAI,
				DisplayName: "zai",
				APIKey:      key,
				// The CODING PLAN endpoint. Note the path: /api/coding/paas/v4, NOT the
				// general /api/paas/v4. A coding-plan key against the general endpoint
				// is rejected, and the two differ only by a path segment — exactly the
				// kind of near-miss that reads as "the model is broken".
				BaseURL: envNonEmpty("ZAI_BASE_URL", "https://api.z.ai/api/coding/paas/v4"),
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
	case strings.HasPrefix(lower, "gemini-") || strings.HasPrefix(lower, "gemini/"):
		if key := envNonEmpty("GOOGLE_API_KEY"); key != "" {
			return &ProviderConfig{
				Kind:        ProviderGemini,
				DisplayName: "gemini",
				APIKey:      key,
				BaseURL:     envNonEmpty("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai"),
			}, true
		}
		if key := envNonEmpty("GEMINI_API_KEY"); key != "" {
			return &ProviderConfig{
				Kind:        ProviderGemini,
				DisplayName: "gemini",
				APIKey:      key,
				BaseURL:     envNonEmpty("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai"),
			}, true
		}
		return nil, true
	case strings.HasPrefix(lower, "deepseek"):
		if key := envNonEmpty("DEEPSEEK_API_KEY"); key != "" {
			return &ProviderConfig{
				Kind:        ProviderOpenAI,
				DisplayName: "deepseek",
				APIKey:      key,
				BaseURL:     envNonEmpty("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
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
// zaiKey resolves the z.ai (GLM) key from either accepted env var.
//
// Deliberately not envNonEmpty("ZAI_API_KEY", "GLM_API_KEY") — see the note in
// detectFromModel: that helper treats its second argument as a literal DEFAULT, so it
// would happily hand the string "GLM_API_KEY" to the API as a bearer token.
func zaiKey() string {
	if v := os.Getenv("ZAI_API_KEY"); v != "" {
		return v
	}
	return os.Getenv("GLM_API_KEY")
}

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

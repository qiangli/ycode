package api

import (
	"os"
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DHNT_BASE_URL", "DHNT_API_KEY",
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL",
		"OPENAI_API_KEY", "OPENAI_BASE_URL",
		"XAI_API_KEY", "XAI_BASE_URL",
		"DASHSCOPE_API_KEY", "DASHSCOPE_BASE_URL",
		"MOONSHOT_API_KEY", "MOONSHOT_BASE_URL",
		"KIMI_API_KEY", "KIMI_BASE_URL",
		"DEEPSEEK_API_KEY", "DEEPSEEK_BASE_URL",
		"GOOGLE_API_KEY", "GEMINI_API_KEY", "GEMINI_BASE_URL",
	} {
		t.Setenv(key, "")
	}
}

func TestResolveModel(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"opus", "claude-opus-4-6-20250415"},
		{"sonnet", "claude-sonnet-4-6-20250514"},
		{"haiku", "claude-haiku-4-5-20251001"},
		{"Sonnet", "claude-sonnet-4-6-20250514"},
		{"kimi", "kimi-k2.5"},
		{"gpt-4o", "gpt-4o"},
		{"claude-sonnet-4-6-20250514", "claude-sonnet-4-6-20250514"},
	}
	for _, tt := range tests {
		if got := ResolveModel(tt.input); got != tt.want {
			t.Errorf("ResolveModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveModelWithAliases(t *testing.T) {
	tests := []struct {
		input   string
		aliases map[string]string
		want    string
	}{
		// nil aliases falls through to built-in.
		{"sonnet", nil, "claude-sonnet-4-6-20250514"},
		// Config alias overrides built-in.
		{"sonnet", map[string]string{"sonnet": "my-custom-sonnet"}, "my-custom-sonnet"},
		// Config alias chains through built-in (fast → haiku → full ID).
		{"fast", map[string]string{"fast": "haiku"}, "claude-haiku-4-5-20251001"},
		// Config alias for unknown name, no built-in match.
		{"mymodel", map[string]string{"mymodel": "gpt-4o"}, "gpt-4o"},
		// Case insensitive.
		{"Fast", map[string]string{"fast": "opus"}, "claude-opus-4-6-20250415"},
		// No match anywhere.
		{"unknown", map[string]string{"other": "value"}, "unknown"},
	}
	for _, tt := range tests {
		got := ResolveModelWithAliases(tt.input, tt.aliases)
		if got != tt.want {
			t.Errorf("ResolveModelWithAliases(%q, %v) = %q, want %q", tt.input, tt.aliases, got, tt.want)
		}
	}
}

func TestDetectProvider_Anthropic(t *testing.T) {
	clearEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	cfg, err := DetectProvider("claude-sonnet-4-6-20250514")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderAnthropic {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderAnthropic)
	}
	if cfg.APIKey != "sk-ant-test" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "sk-ant-test")
	}
}

func TestDetectProvider_AnthropicBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("ANTHROPIC_BASE_URL", "https://custom.anthropic.example/v1/messages")

	cfg, err := DetectProvider("claude-sonnet-4-6-20250514")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://custom.anthropic.example/v1/messages" {
		t.Errorf("BaseURL = %q, want custom URL", cfg.BaseURL)
	}
}

func TestDetectProvider_OpenAI(t *testing.T) {
	clearEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-openai-test")

	cfg, err := DetectProvider("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderOpenAI)
	}
	if cfg.APIKey != "sk-openai-test" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "sk-openai-test")
	}
}

func TestDetectProvider_OpenAIBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("OPENAI_BASE_URL", "http://localhost:11434/v1")

	cfg, err := DetectProvider("llama3")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderOpenAI)
	}
	if cfg.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("BaseURL = %q, want Ollama URL", cfg.BaseURL)
	}
}

// TestDetectProvider_DHNTBaseURL pins the dhnt-namespaced override:
// when DHNT_BASE_URL is set, ycode routes through it as an
// OpenAI-compatible provider with DHNT_API_KEY as the bearer. The
// model name passes through verbatim — even Ollama-style colon tags
// like "qwen3.5:9b" route via DHNT (rather than triggering the
// model-prefix heuristic) because DHNT_BASE_URL is checked first.
func TestDetectProvider_DHNTBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DHNT_BASE_URL", "https://ai.dhnt.io/v1")
	t.Setenv("DHNT_API_KEY", "eyJ-cloudbox-bearer")

	cfg, err := DetectProvider("qwen3.5:9b")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderOpenAI)
	}
	if cfg.BaseURL != "https://ai.dhnt.io/v1" {
		t.Errorf("BaseURL = %q, want cloudbox URL", cfg.BaseURL)
	}
	if cfg.APIKey != "eyJ-cloudbox-bearer" {
		t.Errorf("APIKey = %q, want cloudbox bearer", cfg.APIKey)
	}
}

// TestDetectProvider_DHNTBeatsOpenAI is the load-bearing precedence
// check: when BOTH DHNT_* and OPENAI_* are set, DHNT wins so a user's
// real OpenAI key stays usable from other tools but ycode still
// routes through cloudbox.
func TestDetectProvider_DHNTBeatsOpenAI(t *testing.T) {
	clearEnv(t)
	t.Setenv("DHNT_BASE_URL", "https://ai.dhnt.io/v1")
	t.Setenv("DHNT_API_KEY", "dhnt-token")
	t.Setenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	t.Setenv("OPENAI_API_KEY", "sk-real-openai")

	cfg, err := DetectProvider("llama3")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://ai.dhnt.io/v1" {
		t.Errorf("DHNT_BASE_URL must win over OPENAI_BASE_URL; got %q", cfg.BaseURL)
	}
	if cfg.APIKey != "dhnt-token" {
		t.Errorf("DHNT_API_KEY must win over OPENAI_API_KEY; got %q", cfg.APIKey)
	}
}

// TestDetectProvider_CommercialModelNoKeyDoesNotFallThroughToDHNT is the
// regression for the "no reachable backend has model deepseek-v4-pro" report:
// a commercial-provider model (deepseek-*) selected without its API key must
// NOT fall through to the DHNT cloudbox pool (which only serves Ollama-tagged
// local models and 404s on commercial IDs). It must fail fast with a clear
// error naming the key to set.
func TestDetectProvider_CommercialModelNoKeyDoesNotFallThroughToDHNT(t *testing.T) {
	clearEnv(t)
	t.Setenv("DHNT_BASE_URL", "https://ai.dhnt.io/v1")
	t.Setenv("DHNT_API_KEY", "dhnt-token")

	_, err := DetectProvider("deepseek-v4-pro")
	if err == nil {
		t.Fatal("expected error, got nil — request would route to cloudbox pool")
	}
	if !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Errorf("error should name the key to set; got %q", err.Error())
	}

	// Sanity: with the key present, it routes straight to DeepSeek even
	// though DHNT is still configured.
	t.Setenv("DEEPSEEK_API_KEY", "sk-deepseek")
	cfg, err := DetectProvider("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("unexpected error with key set: %v", err)
	}
	if cfg.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("BaseURL = %q, want DeepSeek endpoint", cfg.BaseURL)
	}
	if cfg.DisplayName != "deepseek" {
		t.Errorf("DisplayName = %q, want deepseek", cfg.DisplayName)
	}
}

// TestDetectProvider_DHNTBaseURLAlone allows an empty token — cloudbox
// will return 401 on the first /v1 call, which is a clearer error
// than silently falling through to a different provider.
func TestDetectProvider_DHNTBaseURLAlone(t *testing.T) {
	clearEnv(t)
	t.Setenv("DHNT_BASE_URL", "https://ai.dhnt.io/v1")

	cfg, err := DetectProvider("anything")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://ai.dhnt.io/v1" {
		t.Errorf("BaseURL = %q, want cloudbox URL", cfg.BaseURL)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey should be empty when DHNT_API_KEY unset; got %q", cfg.APIKey)
	}
}

func TestDetectProvider_XAI(t *testing.T) {
	clearEnv(t)
	t.Setenv("XAI_API_KEY", "xai-test")

	cfg, err := DetectProvider("grok-3")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderOpenAI)
	}
	if cfg.APIKey != "xai-test" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "xai-test")
	}
}

func TestDetectProvider_DashScope(t *testing.T) {
	clearEnv(t)
	t.Setenv("DASHSCOPE_API_KEY", "ds-test")

	cfg, err := DetectProvider("qwen-plus")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderOpenAI)
	}
}

func TestDetectProvider_Moonshot(t *testing.T) {
	clearEnv(t)
	t.Setenv("MOONSHOT_API_KEY", "ms-test")

	cfg, err := DetectProvider("moonshot-v1-auto")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderOpenAI)
	}
	if cfg.APIKey != "ms-test" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "ms-test")
	}
	if cfg.BaseURL != "https://api.moonshot.ai/v1" {
		t.Errorf("BaseURL = %q, want moonshot URL", cfg.BaseURL)
	}
}

func TestDetectProvider_KimiAlias(t *testing.T) {
	clearEnv(t)
	t.Setenv("MOONSHOT_API_KEY", "ms-test")

	// "kimi" alias resolves to "kimi-k2.5" and detects Moonshot provider.
	cfg, err := DetectProvider("moonshot-v1-auto")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderOpenAI)
	}
}

func TestDetectProvider_Gemini(t *testing.T) {
	clearEnv(t)
	t.Setenv("GOOGLE_API_KEY", "goog-test")

	cfg, err := DetectProvider("gemini-2.5-pro")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderGemini {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderGemini)
	}
	if cfg.APIKey != "goog-test" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "goog-test")
	}
	if cfg.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai" {
		t.Errorf("BaseURL = %q, want Gemini URL", cfg.BaseURL)
	}
}

func TestDetectProvider_GeminiAPIKey(t *testing.T) {
	clearEnv(t)
	t.Setenv("GEMINI_API_KEY", "gem-test")

	cfg, err := DetectProvider("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderGemini {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderGemini)
	}
	if cfg.APIKey != "gem-test" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "gem-test")
	}
}

func TestDetectProvider_GeminiAlias(t *testing.T) {
	clearEnv(t)
	t.Setenv("GOOGLE_API_KEY", "goog-test")

	cfg, err := DetectProvider("gemini-pro")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderGemini {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderGemini)
	}
}

func TestDetectProvider_GeminiCustomBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("GOOGLE_API_KEY", "goog-test")
	t.Setenv("GEMINI_BASE_URL", "https://custom.gemini.example/v1")

	cfg, err := DetectProvider("gemini-2.5-pro")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://custom.gemini.example/v1" {
		t.Errorf("BaseURL = %q, want custom URL", cfg.BaseURL)
	}
}

func TestResolveModel_GeminiAliases(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"gemini-pro", "gemini-2.5-pro"},
		{"gemini-flash", "gemini-2.5-flash"},
	}
	for _, tt := range tests {
		if got := ResolveModel(tt.input); got != tt.want {
			t.Errorf("ResolveModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectProvider_FallbackByKeyPriority(t *testing.T) {
	clearEnv(t)
	// Unknown model, fall back to env var priority.
	t.Setenv("OPENAI_API_KEY", "sk-openai")

	cfg, err := DetectProvider("some-custom-model")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderOpenAI)
	}
}

func TestDetectProvider_NoKeys(t *testing.T) {
	clearEnv(t)
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("XAI_API_KEY")
	os.Unsetenv("DASHSCOPE_API_KEY")
	os.Unsetenv("MOONSHOT_API_KEY")
	os.Unsetenv("OPENAI_BASE_URL")

	// Skip if saved OAuth credentials exist on disk — they provide
	// a valid fallback that this unit test cannot easily override.
	if _, err := resolveOAuthToken(); err == nil {
		t.Skip("skipping: saved OAuth credentials provide a valid fallback")
	}

	_, err := DetectProvider("some-unknown-model")
	if err == nil {
		t.Error("expected error when no API keys set")
	}
}

func TestDetectProvider_MismatchedProvider(t *testing.T) {
	clearEnv(t)
	// Only Anthropic key is set, but model is for Moonshot.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	_, err := DetectProvider("moonshot-v1-auto")
	if err == nil {
		t.Error("expected error: moonshot model should not fall through to anthropic provider")
	}
}

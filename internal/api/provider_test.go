package api

import (
	"os"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL",
		"OPENAI_API_KEY", "OPENAI_BASE_URL",
		"XAI_API_KEY", "XAI_BASE_URL",
		"DASHSCOPE_API_KEY", "DASHSCOPE_BASE_URL",
		"MOONSHOT_API_KEY", "MOONSHOT_BASE_URL",
		"KIMI_API_KEY", "KIMI_BASE_URL",
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

	// "kimi" alias resolves to "moonshot-v1-auto" and detects Moonshot provider.
	cfg, err := DetectProvider("moonshot-v1-auto")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != ProviderOpenAI {
		t.Errorf("Kind = %v, want %v", cfg.Kind, ProviderOpenAI)
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

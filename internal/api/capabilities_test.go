package api

import "testing"

func TestDetectCapabilities_Anthropic(t *testing.T) {
	caps := DetectCapabilities(ProviderAnthropic, "claude-sonnet-4-20250514")
	if !caps.CachingSupported {
		t.Error("expected CachingSupported=true for Anthropic")
	}
	if caps.MaxContextTokens != 200_000 {
		t.Errorf("expected 200000 tokens, got %d", caps.MaxContextTokens)
	}
}

func TestDetectCapabilities_OpenAI(t *testing.T) {
	caps := DetectCapabilities(ProviderOpenAI, "gpt-4o-2024-05-13")
	if caps.CachingSupported {
		t.Error("expected CachingSupported=false for OpenAI")
	}
	if caps.MaxContextTokens != 128_000 {
		t.Errorf("expected 128000 tokens, got %d", caps.MaxContextTokens)
	}
}

func TestDetectCapabilities_Gemini(t *testing.T) {
	caps := DetectCapabilities(ProviderGemini, "gemini-2.5-pro")
	if !caps.CachingSupported {
		t.Error("expected CachingSupported=true for Gemini")
	}
	if caps.MaxContextTokens != 1_000_000 {
		t.Errorf("expected 1000000 tokens, got %d", caps.MaxContextTokens)
	}
}

func TestDetectCapabilities_GeminiFlash(t *testing.T) {
	caps := DetectCapabilities(ProviderGemini, "gemini-2.5-flash")
	if !caps.CachingSupported {
		t.Error("expected CachingSupported=true for Gemini Flash")
	}
	if caps.MaxContextTokens != 1_000_000 {
		t.Errorf("expected 1000000 tokens, got %d", caps.MaxContextTokens)
	}
}

func TestDetectCapabilities_Unknown(t *testing.T) {
	caps := DetectCapabilities("ollama", "llama3")
	if caps.CachingSupported {
		t.Error("expected CachingSupported=false for unknown provider")
	}
	if caps.MaxContextTokens != 0 {
		t.Errorf("expected 0 tokens for unknown, got %d", caps.MaxContextTokens)
	}
}

func TestMergeCapabilities(t *testing.T) {
	base := ProviderCapabilities{CachingSupported: false, MaxContextTokens: 128_000}
	overrides := ProviderCapabilities{CachingSupported: true, MaxContextTokens: 200_000}

	merged := MergeCapabilities(base, overrides)
	if merged.MaxContextTokens != 200_000 {
		t.Errorf("expected overridden MaxContextTokens=200000, got %d", merged.MaxContextTokens)
	}
}

func TestMergeCapabilities_ZeroNoOverride(t *testing.T) {
	base := ProviderCapabilities{CachingSupported: true, MaxContextTokens: 200_000}
	overrides := ProviderCapabilities{} // zero values

	merged := MergeCapabilities(base, overrides)
	if merged.MaxContextTokens != 200_000 {
		t.Errorf("expected base MaxContextTokens preserved, got %d", merged.MaxContextTokens)
	}
}

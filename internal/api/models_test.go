package api

import (
	"context"
	"os"
	"testing"
)

func TestDetectProviderFromModel(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"claude-opus-4-6-20250415", "anthropic"},
		{"claude-sonnet-4-6-20250514", "anthropic"},
		{"gpt-4.1", "openai"},
		{"o3", "openai"},
		{"o1-preview", "openai"},
		{"o4-mini", "openai"},
		{"gemini-2.5-pro", "gemini"},
		{"grok-3", "xai"},
		{"qwen-max", "dashscope"},
		{"kimi-k2.5", "moonshot"},
		{"moonshot-v1-128k", "moonshot"},
		{"llama3.2:3b", "unknown"},
		{"custom-model", "unknown"},
	}
	for _, tt := range tests {
		got := DetectProviderFromModel(tt.model)
		if got != tt.expected {
			t.Errorf("DetectProviderFromModel(%q) = %q, want %q", tt.model, got, tt.expected)
		}
	}
}

func TestDiscoverModels_BuiltinAliases(t *testing.T) {
	models := DiscoverModels(context.Background(), nil, nil)

	// Should include all built-in aliases.
	aliasFound := make(map[string]bool)
	for _, m := range models {
		if m.Source == "builtin" && m.Alias != "" {
			aliasFound[m.Alias] = true
		}
	}
	for alias := range ModelAliases {
		if !aliasFound[alias] {
			t.Errorf("built-in alias %q not found in discovered models", alias)
		}
	}
}

func TestDiscoverModels_ConfigAliases(t *testing.T) {
	configAliases := map[string]string{
		"fast":   "haiku",              // resolves through built-in → should be deduped
		"custom": "my-custom-model-v1", // novel model → should appear
		"local":  "llama3",             // novel model → should appear
	}

	models := DiscoverModels(context.Background(), configAliases, nil)

	// "custom" and "local" should be present as config-sourced models.
	found := make(map[string]bool)
	for _, m := range models {
		if m.Source == "config" {
			found[m.Alias] = true
		}
	}
	if !found["custom"] {
		t.Error("expected config alias 'custom' in discovered models")
	}
	if !found["local"] {
		t.Error("expected config alias 'local' in discovered models")
	}
	// "fast" resolves to haiku's full ID which is already a builtin → should NOT appear as config.
	if found["fast"] {
		t.Error("config alias 'fast' should be deduped against builtin haiku")
	}
}

func TestDiscoverModels_OllamaLister(t *testing.T) {
	fakeLister := func(ctx context.Context) []ModelInfo {
		return []ModelInfo{
			{ID: "llama3.2:3b", Provider: "ollama", Source: "ollama", Size: "2.0 GB"},
			{ID: "mistral:7b", Provider: "ollama", Source: "ollama", Size: "4.1 GB"},
		}
	}

	models := DiscoverModels(context.Background(), nil, fakeLister)

	ollamaCount := 0
	for _, m := range models {
		if m.Source == "ollama" {
			ollamaCount++
		}
	}
	if ollamaCount != 2 {
		t.Errorf("expected 2 ollama models, got %d", ollamaCount)
	}
}

func TestDiscoverModels_NoDuplicates(t *testing.T) {
	models := DiscoverModels(context.Background(), nil, nil)

	seen := make(map[string]int)
	for _, m := range models {
		seen[m.ID]++
		if seen[m.ID] > 1 {
			t.Errorf("duplicate model ID: %q (appeared %d times)", m.ID, seen[m.ID])
		}
	}
}

func TestDiscoverModels_EnvDetection(t *testing.T) {
	// Save and clear all relevant env vars to get a clean baseline.
	envVars := []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GOOGLE_API_KEY",
		"GEMINI_API_KEY", "XAI_API_KEY", "DASHSCOPE_API_KEY",
		"MOONSHOT_API_KEY", "KIMI_API_KEY",
	}
	saved := make(map[string]string)
	for _, k := range envVars {
		saved[k] = os.Getenv(k)
		os.Unsetenv(k)
	}
	t.Cleanup(func() {
		for k, v := range saved {
			if v != "" {
				os.Setenv(k, v)
			} else {
				os.Unsetenv(k)
			}
		}
	})

	t.Run("NoEnvKeys_NoEnvModels", func(t *testing.T) {
		models := DiscoverModels(context.Background(), nil, nil)
		for _, m := range models {
			if m.Source == "env" {
				t.Errorf("unexpected env model %q when no API keys set", m.ID)
			}
		}
	})

	t.Run("AnthropicKey_AddsAnthropicModels", func(t *testing.T) {
		os.Setenv("ANTHROPIC_API_KEY", "test-key")
		defer os.Unsetenv("ANTHROPIC_API_KEY")

		models := DiscoverModels(context.Background(), nil, nil)

		// Anthropic models from env should be deduped against builtins.
		// Built-in aliases already cover claude-opus, sonnet, haiku — so
		// env detection should NOT add duplicates.
		envCount := 0
		for _, m := range models {
			if m.Source == "env" && m.Provider == "anthropic" {
				envCount++
			}
		}
		if envCount != 0 {
			t.Errorf("expected 0 env anthropic models (all deduped against builtins), got %d", envCount)
		}
	})

	t.Run("OpenAIKey_AddsOpenAIModels", func(t *testing.T) {
		os.Setenv("OPENAI_API_KEY", "test-key")
		defer os.Unsetenv("OPENAI_API_KEY")

		models := DiscoverModels(context.Background(), nil, nil)

		envModels := make(map[string]bool)
		for _, m := range models {
			if m.Source == "env" && m.Provider == "openai" {
				envModels[m.ID] = true
			}
		}
		if !envModels["gpt-4.1"] {
			t.Error("expected gpt-4.1 from env detection")
		}
		if !envModels["o3"] {
			t.Error("expected o3 from env detection")
		}
	})

	t.Run("GeminiKey_AddsGeminiModels", func(t *testing.T) {
		os.Setenv("GEMINI_API_KEY", "test-key")
		defer os.Unsetenv("GEMINI_API_KEY")

		models := DiscoverModels(context.Background(), nil, nil)

		// gemini-2.5-pro and gemini-2.5-flash are also in builtins,
		// so they should be deduped.
		envCount := 0
		for _, m := range models {
			if m.Source == "env" && m.Provider == "gemini" {
				envCount++
			}
		}
		if envCount != 0 {
			t.Errorf("expected 0 env gemini models (deduped against builtins), got %d", envCount)
		}
	})

	t.Run("XAIKey_AddsGrokModels", func(t *testing.T) {
		os.Setenv("XAI_API_KEY", "test-key")
		defer os.Unsetenv("XAI_API_KEY")

		models := DiscoverModels(context.Background(), nil, nil)

		envModels := make(map[string]bool)
		for _, m := range models {
			if m.Source == "env" && m.Provider == "xai" {
				envModels[m.ID] = true
			}
		}
		if !envModels["grok-3"] {
			t.Error("expected grok-3 from env detection")
		}
	})

	t.Run("MultipleKeys_AllProviders", func(t *testing.T) {
		os.Setenv("OPENAI_API_KEY", "test-key")
		os.Setenv("XAI_API_KEY", "test-key")
		os.Setenv("DASHSCOPE_API_KEY", "test-key")
		defer func() {
			os.Unsetenv("OPENAI_API_KEY")
			os.Unsetenv("XAI_API_KEY")
			os.Unsetenv("DASHSCOPE_API_KEY")
		}()

		models := DiscoverModels(context.Background(), nil, nil)

		providers := make(map[string]bool)
		for _, m := range models {
			if m.Source == "env" {
				providers[m.Provider] = true
			}
		}
		for _, want := range []string{"openai", "xai", "dashscope"} {
			if !providers[want] {
				t.Errorf("expected env provider %q, got %v", want, providers)
			}
		}
	})

	t.Run("DuplicateProviderKeys_NoDuplicates", func(t *testing.T) {
		// Both GOOGLE_API_KEY and GEMINI_API_KEY map to gemini provider.
		os.Setenv("GOOGLE_API_KEY", "test-key")
		os.Setenv("GEMINI_API_KEY", "test-key")
		defer func() {
			os.Unsetenv("GOOGLE_API_KEY")
			os.Unsetenv("GEMINI_API_KEY")
		}()

		models := DiscoverModels(context.Background(), nil, nil)

		seen := make(map[string]int)
		for _, m := range models {
			seen[m.ID]++
			if seen[m.ID] > 1 {
				t.Errorf("duplicate model %q", m.ID)
			}
		}
	})
}

func TestDiscoverModels_OllamaDeduplication(t *testing.T) {
	// If an Ollama model has the same ID as a builtin, it should be deduped.
	fakeLister := func(ctx context.Context) []ModelInfo {
		return []ModelInfo{
			// This ID matches a builtin alias target → should be deduped.
			{ID: "claude-sonnet-4-6-20250514", Provider: "ollama", Source: "ollama"},
			// This is unique → should appear.
			{ID: "codellama:13b", Provider: "ollama", Source: "ollama", Size: "7.4 GB"},
		}
	}

	models := DiscoverModels(context.Background(), nil, fakeLister)

	ollamaCount := 0
	for _, m := range models {
		if m.Source == "ollama" {
			ollamaCount++
		}
	}
	if ollamaCount != 1 {
		t.Errorf("expected 1 ollama model (deduped), got %d", ollamaCount)
	}
}

func TestDiscoverModels_AllSourcesTogether(t *testing.T) {
	// Save and set env.
	saved := os.Getenv("OPENAI_API_KEY")
	os.Setenv("OPENAI_API_KEY", "test-key")
	defer func() {
		if saved != "" {
			os.Setenv("OPENAI_API_KEY", saved)
		} else {
			os.Unsetenv("OPENAI_API_KEY")
		}
	}()

	configAliases := map[string]string{
		"my-local": "phi3:mini",
	}
	fakeLister := func(ctx context.Context) []ModelInfo {
		return []ModelInfo{
			{ID: "llama3.2:3b", Provider: "ollama", Source: "ollama", Size: "2.0 GB"},
		}
	}

	models := DiscoverModels(context.Background(), configAliases, fakeLister)

	sources := make(map[string]int)
	for _, m := range models {
		sources[m.Source]++
	}

	if sources["builtin"] == 0 {
		t.Error("expected builtin models")
	}
	if sources["config"] == 0 {
		t.Error("expected config models")
	}
	if sources["env"] == 0 {
		t.Error("expected env models")
	}
	if sources["ollama"] == 0 {
		t.Error("expected ollama models")
	}

	// Verify no duplicates across all sources.
	seen := make(map[string]int)
	for _, m := range models {
		seen[m.ID]++
		if seen[m.ID] > 1 {
			t.Errorf("duplicate model ID %q across sources", m.ID)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{1024, "1024 B"},
		{5 * 1024 * 1024, "5 MB"},
		{4080218931, "3.8 GB"},
	}
	for _, tt := range tests {
		got := FormatBytes(tt.input)
		if got != tt.expected {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

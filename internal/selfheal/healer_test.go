package selfheal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/api"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected FailureType
	}{
		{
			name:     "build error - syntax",
			err:      errors.New("syntax error: unexpected newline"),
			expected: FailureTypeBuild,
		},
		{
			name:     "build error - undefined",
			err:      errors.New("undefined: someVariable"),
			expected: FailureTypeBuild,
		},
		{
			name:     "config error",
			err:      errors.New("failed to load config file"),
			expected: FailureTypeConfig,
		},
		{
			name:     "API error - connection",
			err:      errors.New("connection timeout"),
			expected: FailureTypeAPI,
		},
		{
			name:     "runtime error - panic",
			err:      errors.New("panic: runtime error: nil pointer dereference"),
			expected: FailureTypeRuntime,
		},
		{
			name:     "tool error",
			err:      errors.New("tool execution failed"),
			expected: FailureTypeTool,
		},
		{
			name:     "unknown error",
			err:      errors.New("something completely unexpected"),
			expected: FailureTypeUnknown,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: FailureTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			if result != tt.expected {
				t.Errorf("ClassifyError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestHealer_CanHeal(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		err      error
		expected bool
	}{
		{
			name:     "enabled and healable error",
			config:   &Config{Enabled: true, MaxAttempts: 3},
			err:      errors.New("build failed"),
			expected: true,
		},
		{
			name:     "disabled",
			config:   &Config{Enabled: false},
			err:      errors.New("build failed"),
			expected: false,
		},
		{
			name:     "nil error",
			config:   &Config{Enabled: true},
			err:      nil,
			expected: false,
		},
		{
			name:     "max attempts exceeded",
			config:   &Config{Enabled: true, MaxAttempts: 1},
			err:      errors.New("connection timeout"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHealer(tt.config)

			// For max attempts test, consume the one allowed attempt
			if tt.name == "max attempts exceeded" {
				errInfo := ErrorInfo{
					Type:      FailureTypeAPI,
					Error:     tt.err,
					Timestamp: time.Now(),
				}
				_, _ = h.AttemptHealing(context.Background(), errInfo)
			}

			result := h.CanHeal(tt.err)
			if result != tt.expected {
				t.Errorf("CanHeal() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHealer_AttemptHealing(t *testing.T) {
	config := &Config{
		Enabled:      true,
		MaxAttempts:  3,
		AutoRebuild:  false,
		AutoRestart:  false,
		BuildCommand: "",
	}

	t.Run("successful healing - API error", func(t *testing.T) {
		h := NewHealer(config)
		errInfo := ErrorInfo{
			Type:      FailureTypeAPI,
			Error:     errors.New("connection timeout"),
			Message:   "connection timeout",
			Timestamp: time.Now(),
		}

		success, err := h.AttemptHealing(context.Background(), errInfo)
		if err != nil {
			t.Errorf("AttemptHealing returned error: %v", err)
		}
		if !success {
			t.Error("AttemptHealing should have succeeded for API error")
		}

		if h.State() != HealerStateSucceeded {
			t.Errorf("Expected state %v, got %v", HealerStateSucceeded, h.State())
		}
	})

	t.Run("build error requires AI integration", func(t *testing.T) {
		h := NewHealer(config)
		errInfo := ErrorInfo{
			Type:      FailureTypeBuild,
			Error:     errors.New("syntax error: unexpected newline"),
			Message:   "syntax error: unexpected newline",
			Timestamp: time.Now(),
		}

		success, err := h.AttemptHealing(context.Background(), errInfo)
		if err == nil {
			t.Error("Expected error for unimplemented build healing")
		}
		if success {
			t.Error("Build healing should not succeed without AI integration")
		}
	})

	t.Run("max attempts reached", func(t *testing.T) {
		h := NewHealer(&Config{
			Enabled:     true,
			MaxAttempts: 1,
			AutoRebuild: false,
			AutoRestart: false,
		})

		errInfo := ErrorInfo{
			Type:      FailureTypeAPI,
			Error:     errors.New("connection timeout"),
			Message:   "connection timeout",
			Timestamp: time.Now(),
		}

		// First attempt succeeds (API error)
		_, _ = h.AttemptHealing(context.Background(), errInfo)

		// Second attempt should fail due to max attempts
		success, err := h.AttemptHealing(context.Background(), errInfo)
		if err == nil {
			t.Error("Expected error for max attempts exceeded")
		}
		if success {
			t.Error("Expected failure after max attempts")
		}
	})

	t.Run("disabled healer", func(t *testing.T) {
		h := NewHealer(&Config{Enabled: false})
		errInfo := ErrorInfo{
			Type:      FailureTypeBuild,
			Error:     errors.New("build failed"),
			Timestamp: time.Now(),
		}

		success, err := h.AttemptHealing(context.Background(), errInfo)
		if err == nil {
			t.Error("Expected error when healing is disabled")
		}
		if success {
			t.Error("Expected failure when healing is disabled")
		}
	})
}

func TestHealer_GetAttempts(t *testing.T) {
	h := NewHealer(&Config{Enabled: true, MaxAttempts: 3})

	if len(h.GetAttempts(FailureTypeAPI)) != 0 {
		t.Error("Expected 0 attempts initially")
	}

	errInfo := ErrorInfo{
		Type:      FailureTypeAPI,
		Error:     errors.New("connection timeout"),
		Timestamp: time.Now(),
	}
	_, _ = h.AttemptHealing(context.Background(), errInfo)

	attempts := h.GetAttempts(FailureTypeAPI)
	if len(attempts) != 1 {
		t.Errorf("Expected 1 attempt, got %d", len(attempts))
	}

	if attempts[0].AttemptNumber != 1 {
		t.Errorf("Expected attempt number 1, got %d", attempts[0].AttemptNumber)
	}
}

func TestHealer_Reset(t *testing.T) {
	h := NewHealer(&Config{Enabled: true, MaxAttempts: 3})

	errInfo := ErrorInfo{
		Type:      FailureTypeAPI,
		Error:     errors.New("connection timeout"),
		Timestamp: time.Now(),
	}
	_, _ = h.AttemptHealing(context.Background(), errInfo)

	h.Reset()

	if len(h.GetAttempts(FailureTypeAPI)) != 0 {
		t.Error("Expected attempts to be cleared after reset")
	}
	if len(h.GetHistory()) != 0 {
		t.Error("Expected history to be cleared after reset")
	}
	if h.State() != HealerStateIdle {
		t.Errorf("Expected state to be idle after reset, got %v", h.State())
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Enabled {
		t.Error("Expected Enabled to be true by default")
	}
	if cfg.MaxAttempts != 3 {
		t.Errorf("Expected MaxAttempts to be 3, got %d", cfg.MaxAttempts)
	}
	if !cfg.AutoRebuild {
		t.Error("Expected AutoRebuild to be true by default")
	}
	if !cfg.AutoRestart {
		t.Error("Expected AutoRestart to be true by default")
	}
	if cfg.BuildTimeout == 0 {
		t.Error("Expected BuildTimeout to be set")
	}
}

func TestHealer_findProjectRoot(t *testing.T) {
	h := NewHealer(DefaultConfig())

	root := h.findProjectRoot()
	if root == "" {
		t.Error("Expected non-empty project root")
	}
}

func TestWrapMain(t *testing.T) {
	t.Run("successful execution", func(t *testing.T) {
		called := false
		mainFn := func() error {
			called = true
			return nil
		}

		exitCode := WrapMain(mainFn, &Config{Enabled: false})
		if exitCode != 0 {
			t.Errorf("Expected exit code 0, got %d", exitCode)
		}
		if !called {
			t.Error("Main function was not called")
		}
	})

	t.Run("error without healing", func(t *testing.T) {
		mainFn := func() error {
			return errors.New("test error")
		}

		exitCode := WrapMain(mainFn, &Config{Enabled: false})
		if exitCode != 1 {
			t.Errorf("Expected exit code 1, got %d", exitCode)
		}
	})

	t.Run("healing disabled for error type", func(t *testing.T) {
		mainFn := func() error {
			return errors.New("some unhealable error type xyz")
		}

		exitCode := WrapMain(mainFn, DefaultConfig())
		if exitCode != 1 {
			t.Errorf("Expected exit code 1, got %d", exitCode)
		}
	})
}

func TestWrapMainWithOptions(t *testing.T) {
	t.Run("with provider", func(t *testing.T) {
		mainFn := func() error {
			return nil
		}

		exitCode := WrapMainWithOptions(mainFn, &WrapMainOptions{
			Config:   &Config{Enabled: true, MaxAttempts: 3},
			Provider: &mockProvider{},
		})
		if exitCode != 0 {
			t.Errorf("Expected exit code 0, got %d", exitCode)
		}
	})

	t.Run("nil options", func(t *testing.T) {
		mainFn := func() error {
			return nil
		}

		exitCode := WrapMainWithOptions(mainFn, nil)
		if exitCode != 0 {
			t.Errorf("Expected exit code 0, got %d", exitCode)
		}
	})
}

func TestRunWithHealing(t *testing.T) {
	t.Run("successful execution", func(t *testing.T) {
		called := false
		fn := func() error {
			called = true
			return nil
		}

		err := RunWithHealing(context.Background(), fn, &Config{Enabled: false})
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !called {
			t.Error("Function was not called")
		}
	})

	t.Run("error propagation", func(t *testing.T) {
		fn := func() error {
			return errors.New("original error")
		}

		err := RunWithHealing(context.Background(), fn, &Config{Enabled: false})
		if err == nil {
			t.Error("Expected error to be returned")
		}
	})
}

func TestHealer_SetAIHealer(t *testing.T) {
	h := NewHealer(DefaultConfig())
	if h.aiHealer != nil {
		t.Error("Expected nil aiHealer initially")
	}

	ah := NewAIHealer(nil, &mockProvider{})
	h.SetAIHealer(ah)

	if h.aiHealer == nil {
		t.Error("Expected non-nil aiHealer after SetAIHealer")
	}
}

// --- AI Healer tests ---

// mockProvider implements api.Provider for testing.
type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Send(_ context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent, 10)
	errc := make(chan error, 1)

	go func() {
		defer close(events)
		if m.err != nil {
			errc <- m.err
			return
		}

		if m.response != "" {
			delta, _ := json.Marshal(struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text_delta", Text: m.response})

			events <- &api.StreamEvent{
				Type:  "content_block_delta",
				Delta: delta,
			}
		}
		errc <- nil
	}()

	return events, errc
}

func (m *mockProvider) Kind() api.ProviderKind {
	return api.ProviderAnthropic
}

func TestCollectStreamingText(t *testing.T) {
	t.Run("collects text", func(t *testing.T) {
		events := make(chan *api.StreamEvent, 10)
		errc := make(chan error, 1)

		delta1, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "Hello "})
		delta2, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "World"})

		events <- &api.StreamEvent{Type: "content_block_delta", Delta: delta1}
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: delta2}
		close(events)
		errc <- nil

		text, err := collectStreamingText(events, errc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if text != "Hello World" {
			t.Errorf("expected 'Hello World', got %q", text)
		}
	})

	t.Run("handles stream error", func(t *testing.T) {
		events := make(chan *api.StreamEvent)
		errc := make(chan error, 1)

		close(events)
		errc <- fmt.Errorf("connection reset")

		_, err := collectStreamingText(events, errc)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("skips non-delta events", func(t *testing.T) {
		events := make(chan *api.StreamEvent, 10)
		errc := make(chan error, 1)

		events <- &api.StreamEvent{Type: "message_start"}
		delta, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "only this"})
		events <- &api.StreamEvent{Type: "content_block_delta", Delta: delta}
		events <- &api.StreamEvent{Type: "message_stop"}
		close(events)
		errc <- nil

		text, err := collectStreamingText(events, errc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if text != "only this" {
			t.Errorf("expected 'only this', got %q", text)
		}
	})
}

func TestAIHealer_parseFixResponse(t *testing.T) {
	ah := NewAIHealer(nil, nil)

	t.Run("raw JSON", func(t *testing.T) {
		input := `[{"path":"foo.go","description":"fix","original":"old","modified":"new"}]`
		fixes, err := ah.parseFixResponse(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(fixes) != 1 {
			t.Fatalf("expected 1 fix, got %d", len(fixes))
		}
		if fixes[0].Path != "foo.go" {
			t.Errorf("expected path 'foo.go', got %q", fixes[0].Path)
		}
	})

	t.Run("markdown code block", func(t *testing.T) {
		input := "Here's the fix:\n\n```json\n[{\"path\":\"bar.go\",\"description\":\"fix\",\"original\":\"x\",\"modified\":\"y\"}]\n```\n"
		fixes, err := ah.parseFixResponse(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(fixes) != 1 {
			t.Fatalf("expected 1 fix, got %d", len(fixes))
		}
		if fixes[0].Path != "bar.go" {
			t.Errorf("expected path 'bar.go', got %q", fixes[0].Path)
		}
	})

	t.Run("empty array", func(t *testing.T) {
		input := "```json\n[]\n```"
		fixes, err := ah.parseFixResponse(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(fixes) != 0 {
			t.Errorf("expected 0 fixes, got %d", len(fixes))
		}
	})

	t.Run("unparseable", func(t *testing.T) {
		input := "I don't know how to fix this."
		_, err := ah.parseFixResponse(input)
		if err == nil {
			t.Error("expected error for unparseable response")
		}
	})
}

func TestAIHealer_parseBuildErrors(t *testing.T) {
	ah := NewAIHealer(nil, nil)

	output := `internal/foo.go:42:10: undefined: bar
internal/baz.go:7:1: syntax error: unexpected newline
not a build error
`
	errors := ah.parseBuildErrors(output)
	if len(errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errors))
	}

	if errors[0].File != "internal/foo.go" || errors[0].Line != 42 || errors[0].Column != 10 {
		t.Errorf("unexpected first error: %+v", errors[0])
	}
	if errors[1].File != "internal/baz.go" || errors[1].Line != 7 {
		t.Errorf("unexpected second error: %+v", errors[1])
	}
}

func TestAIHealer_isPathHealable(t *testing.T) {
	ah := NewAIHealer(&AIConfig{
		Config: &Config{
			HealablePaths:  []string{"*.go", "go.mod"},
			ProtectedPaths: []string{".git/", "vendor/"},
		},
	}, nil)

	tests := []struct {
		path     string
		expected bool
	}{
		{"main.go", true},
		{"internal/foo.go", true}, // matches *.go via basename
		{"go.mod", true},
		{".git/config", false},   // protected
		{"vendor/lib.go", false}, // protected
		{"README.md", false},     // not in healable paths
		{"data.json", false},     // not in healable paths
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := ah.isPathHealable(tt.path)
			if result != tt.expected {
				t.Errorf("isPathHealable(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestAIHealer_applyFixes(t *testing.T) {
	ah := NewAIHealer(&AIConfig{
		Config: &Config{
			HealablePaths:  []string{"*.go"},
			ProtectedPaths: []string{},
		},
	}, nil)

	t.Run("replace content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.go")
		os.WriteFile(path, []byte("package main\n\nfunc foo() {}\n"), 0644)

		err := ah.applyFixes([]FileFix{
			{
				Path:     path,
				Original: "func foo() {}",
				Modified: "func foo() int { return 42 }",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, _ := os.ReadFile(path)
		if got := string(content); got != "package main\n\nfunc foo() int { return 42 }\n" {
			t.Errorf("unexpected content: %q", got)
		}
	})

	t.Run("create new file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "subdir", "new.go")

		err := ah.applyFixes([]FileFix{
			{
				Path:     path,
				Original: "",
				Modified: "package subdir\n",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, _ := os.ReadFile(path)
		if string(content) != "package subdir\n" {
			t.Errorf("unexpected content: %q", string(content))
		}
	})

	t.Run("original not found", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.go")
		os.WriteFile(path, []byte("package main\n"), 0644)

		err := ah.applyFixes([]FileFix{
			{
				Path:     path,
				Original: "nonexistent content",
				Modified: "replacement",
			},
		})
		if err == nil {
			t.Error("expected error when original content not found")
		}
	})

	t.Run("protected path rejected", func(t *testing.T) {
		ah2 := NewAIHealer(&AIConfig{
			Config: &Config{
				HealablePaths:  []string{"*.go"},
				ProtectedPaths: []string{".git/"},
			},
		}, nil)

		err := ah2.applyFixes([]FileFix{
			{
				Path:     ".git/config",
				Original: "old",
				Modified: "new",
			},
		})
		if err == nil {
			t.Error("expected error for protected path")
		}
	})
}

func TestAIHealer_AttemptAIFixing(t *testing.T) {
	t.Run("no provider", func(t *testing.T) {
		ah := NewAIHealer(nil, nil)
		_, err := ah.AttemptAIFixing(context.Background(), ErrorInfo{
			Type:  FailureTypeBuild,
			Error: errors.New("build failed"),
		})
		if err == nil {
			t.Error("expected error with nil provider")
		}
	})

	t.Run("empty fix array", func(t *testing.T) {
		provider := &mockProvider{response: "```json\n[]\n```"}
		ah := NewAIHealer(nil, provider)

		_, err := ah.AttemptAIFixing(context.Background(), ErrorInfo{
			Type:    FailureTypeBuild,
			Error:   errors.New("build failed"),
			Message: "build failed",
		})
		if err == nil {
			t.Error("expected error for empty fixes")
		}
	})

	t.Run("provider error", func(t *testing.T) {
		provider := &mockProvider{err: fmt.Errorf("API unavailable")}
		ah := NewAIHealer(nil, provider)

		_, err := ah.AttemptAIFixing(context.Background(), ErrorInfo{
			Type:    FailureTypeBuild,
			Error:   errors.New("build failed"),
			Message: "build failed",
		})
		if err == nil {
			t.Error("expected error when provider fails")
		}
	})
}

func TestAIHealer_buildUserPrompt(t *testing.T) {
	ah := NewAIHealer(nil, nil)

	errInfo := ErrorInfo{
		Type:       FailureTypeBuild,
		Error:      errors.New("undefined: foo"),
		Message:    "main.go:10:5: undefined: foo",
		StackTrace: "goroutine 1 [running]:\nmain.main()\n",
		Context:    map[string]string{"file": "main.go"},
	}

	prompt := ah.buildUserPrompt(errInfo, &FixAttempt{Iteration: 1})

	if !contains(prompt, "build") {
		t.Error("expected prompt to contain error type")
	}
	if !contains(prompt, "undefined: foo") {
		t.Error("expected prompt to contain error message")
	}
	if !contains(prompt, "goroutine") {
		t.Error("expected prompt to contain stack trace")
	}
}

func TestHealer_fixWithAI_delegation(t *testing.T) {
	t.Run("delegates to AI when available", func(t *testing.T) {
		// Mock provider returns an empty fix array, which will cause
		// AttemptAIFixing to fail with "cannot be fixed" error
		provider := &mockProvider{response: "```json\n[]\n```"}
		ah := NewAIHealer(nil, provider)

		h := NewHealer(&Config{Enabled: true, MaxAttempts: 3})
		h.SetAIHealer(ah)

		attempt := &HealingAttempt{Actions: make([]HealingAction, 0)}
		errInfo := ErrorInfo{
			Type:    FailureTypeBuild,
			Error:   errors.New("build failed"),
			Message: "build failed",
		}

		err := h.fixBuildError(context.Background(), attempt, errInfo)
		if err == nil {
			t.Error("expected error (empty fixes)")
		}

		// Should have an ai_fix action
		found := false
		for _, a := range attempt.Actions {
			if a.Type == "ai_fix" {
				found = true
			}
		}
		if !found {
			t.Error("expected ai_fix action in attempt")
		}
	})

	t.Run("falls back without AI", func(t *testing.T) {
		h := NewHealer(&Config{Enabled: true, MaxAttempts: 3})

		attempt := &HealingAttempt{Actions: make([]HealingAction, 0)}
		errInfo := ErrorInfo{
			Type:    FailureTypeBuild,
			Error:   errors.New("build failed"),
			Message: "build failed",
		}

		err := h.fixBuildError(context.Background(), attempt, errInfo)
		if err == nil {
			t.Error("expected error without AI provider")
		}
		if !contains(err.Error(), "no AI provider") {
			t.Errorf("expected 'no AI provider' in error, got: %v", err)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func ExampleHealer() {
	config := DefaultConfig()
	healer := NewHealer(config)

	errInfo := ErrorInfo{
		Type:      FailureTypeAPI,
		Error:     fmt.Errorf("connection timeout"),
		Message:   "connection timeout after 30s",
		Timestamp: time.Now(),
	}

	success, err := healer.AttemptHealing(context.Background(), errInfo)
	if err != nil {
		fmt.Printf("Healing failed: %v\n", err)
		return
	}

	if success {
		fmt.Println("Self-healing succeeded!")
	}
	// Output: Self-healing succeeded!
}

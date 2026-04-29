package browseruse

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAction_JSON(t *testing.T) {
	action := Action{
		Type:      "navigate",
		URL:       "https://example.com",
		ElementID: 0,
	}

	data, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Action
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Type != "navigate" {
		t.Errorf("Type = %q, want %q", decoded.Type, "navigate")
	}
	if decoded.URL != "https://example.com" {
		t.Errorf("URL = %q, want %q", decoded.URL, "https://example.com")
	}
}

func TestResult_JSON(t *testing.T) {
	result := Result{
		Success:  true,
		Title:    "Example",
		URL:      "https://example.com",
		Content:  "Hello World",
		Elements: "[1] <a href=\"/\">Home</a>",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Result
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !decoded.Success {
		t.Error("expected Success = true")
	}
	if decoded.Title != "Example" {
		t.Errorf("Title = %q, want %q", decoded.Title, "Example")
	}
}

func TestService_NotAvailable(t *testing.T) {
	svc := &Service{}
	if svc.Available() {
		t.Error("expected Available() = false for unstarted service")
	}

	_, err := svc.Execute(context.Background(), Action{Type: "navigate", URL: "https://example.com"})
	if err == nil {
		t.Error("expected error when executing with unavailable service")
	}
}

func TestEscapeShell(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"it's", "it'\\''s"},
		{"no quotes", "no quotes"},
	}

	for _, tt := range tests {
		got := escapeShell(tt.input)
		if got != tt.want {
			t.Errorf("escapeShell(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDockerfile_NotEmpty(t *testing.T) {
	if len(dockerfile) == 0 {
		t.Error("dockerfile should not be empty")
	}
	if len(entrypointPy) == 0 {
		t.Error("entrypointPy should not be empty")
	}
}

package redact

import (
	"testing"
)

func TestRedactor_APIKeys(t *testing.T) {
	r := DefaultPatterns()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "anthropic key",
			input: "using key sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456",
			want:  "using key [REDACTED:anthropic_key]",
		},
		{
			name:  "openai key",
			input: "OPENAI_API_KEY=sk-proj1234567890abcdefghij",
			want:  "OPENAI_API_KEY=[REDACTED:openai_key]",
		},
		{
			name:  "bearer token",
			input: "Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.long.token",
			want:  "Authorization: Bearer [REDACTED]",
		},
		{
			name:  "github token",
			input: "GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijkl",
			want:  "GITHUB_TOKEN=[REDACTED:github_token]",
		},
		{
			name:  "aws access key",
			input: "key: AKIAIOSFODNN7EXAMPLE",
			want:  "key: [REDACTED:aws_key]",
		},
		{
			name:  "no secrets",
			input: "Hello, this is a normal message with no secrets.",
			want:  "Hello, this is a normal message with no secrets.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Redact(tt.input)
			if got != tt.want {
				t.Errorf("Redact(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRedactor_ContainsSensitive(t *testing.T) {
	r := DefaultPatterns()

	if !r.ContainsSensitive("my key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456") {
		t.Error("expected sensitive content to be detected")
	}
	if r.ContainsSensitive("just a normal string") {
		t.Error("expected no sensitive content")
	}
}

func TestRedactor_RedactMap(t *testing.T) {
	r := DefaultPatterns()

	m := map[string]any{
		"message": "key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456",
		"count":   42,
		"safe":    "hello",
	}

	out := r.RedactMap(m)
	if out["message"] != "key is [REDACTED:anthropic_key]" {
		t.Errorf("expected redacted message, got %v", out["message"])
	}
	if out["count"] != 42 {
		t.Error("non-string values should pass through")
	}
	if out["safe"] != "hello" {
		t.Error("safe strings should pass through")
	}
}

func TestRedactor_Email(t *testing.T) {
	r := DefaultPatterns()
	got := r.Redact("contact user@example.com for details")
	want := "contact [REDACTED:email] for details"
	if got != want {
		t.Errorf("Redact email = %q, want %q", got, want)
	}
}

func TestNew_InvalidRegex(t *testing.T) {
	// Invalid regex should be silently skipped.
	r := New([]Pattern{
		{Name: "bad", Regex: `[invalid`, Replacement: "x"},
		{Name: "good", Regex: `secret`, Replacement: "REDACTED"},
	})
	got := r.Redact("my secret value")
	if got != "my REDACTED value" {
		t.Errorf("expected 'my REDACTED value', got %q", got)
	}
}

func TestRedactor_NilMap(t *testing.T) {
	r := DefaultPatterns()
	if r.RedactMap(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

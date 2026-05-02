package agentdef

import (
	"encoding/json"
	"testing"
)

func TestDefaultOutputValidator_ValidJSON(t *testing.T) {
	v := &DefaultOutputValidator{}
	schema := &OutputSchema{
		RequiredFields: []string{"summary", "score"},
		JSONSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"summary": {"type": "string"},
				"score": {"type": "number"}
			},
			"required": ["summary", "score"]
		}`),
	}

	result, err := v.Validate(`{"summary": "test output", "score": 0.95}`, schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid result, got errors: %v", result.Errors)
	}
}

func TestDefaultOutputValidator_MissingField(t *testing.T) {
	v := &DefaultOutputValidator{}
	schema := &OutputSchema{
		RequiredFields: []string{"summary", "score"},
	}

	result, err := v.Validate(`{"summary": "test output"}`, schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid result")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(result.Errors), result.Errors)
	}
}

func TestDefaultOutputValidator_InvalidJSON(t *testing.T) {
	v := &DefaultOutputValidator{}
	schema := &OutputSchema{
		RequiredFields: []string{"x"},
	}

	result, err := v.Validate("not json at all", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid result for non-JSON")
	}
}

func TestDefaultOutputValidator_CodeFence(t *testing.T) {
	v := &DefaultOutputValidator{}
	schema := &OutputSchema{
		RequiredFields: []string{"ok"},
	}

	input := "Here is the result:\n```json\n{\"ok\": true}\n```\nDone."
	result, err := v.Validate(input, schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
}

func TestDefaultOutputValidator_NilSchema(t *testing.T) {
	v := &DefaultOutputValidator{}
	result, err := v.Validate("anything", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatal("nil schema should pass")
	}
}

func TestDefaultOutputValidator_TypeMismatch(t *testing.T) {
	v := &DefaultOutputValidator{}
	schema := &OutputSchema{
		JSONSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"count": {"type": "number"}
			}
		}`),
	}

	result, err := v.Validate(`{"count": "not-a-number"}`, schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid due to type mismatch")
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"raw object", `{"a": 1}`, `{"a": 1}`},
		{"raw array", `[1, 2]`, `[1, 2]`},
		{"code fence", "```json\n{\"a\": 1}\n```", `{"a": 1}`},
		{"plain fence", "```\n{\"a\": 1}\n```", `{"a": 1}`},
		{"no json", "hello world", ""},
		{"wrapped", "result:\n```json\n{\"x\": true}\n```\ndone", `{"x": true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatSchemaPrompt(t *testing.T) {
	schema := &OutputSchema{
		JSONSchema:     json.RawMessage(`{"type": "object"}`),
		RequiredFields: []string{"name"},
	}
	prompt := FormatSchemaPrompt(schema)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}

	if FormatSchemaPrompt(nil) != "" {
		t.Fatal("nil schema should produce empty prompt")
	}
}

package swarm

import (
	"testing"
)

func TestDetectHandoff_Valid(t *testing.T) {
	signal := `{"__handoff__":true,"result":{"target_agent":"helper","context_vars":{"key":"val"},"message":"do this"}}`

	hr, ok := DetectHandoff(signal)
	if !ok {
		t.Fatal("expected handoff to be detected")
	}
	if hr.TargetAgent != "helper" {
		t.Errorf("target_agent = %q, want %q", hr.TargetAgent, "helper")
	}
	if hr.ContextVars["key"] != "val" {
		t.Errorf("context_vars[key] = %q, want %q", hr.ContextVars["key"], "val")
	}
	if hr.Message != "do this" {
		t.Errorf("message = %q, want %q", hr.Message, "do this")
	}
}

func TestDetectHandoff_NotAHandoff(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"plain text", "just some text"},
		{"valid json no marker", `{"result":"hello"}`},
		{"marker false", `{"__handoff__":false,"result":{"target_agent":"x"}}`},
		{"empty target", `{"__handoff__":true,"result":{"target_agent":""}}`},
		{"invalid json", `{invalid`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := DetectHandoff(tt.input)
			if ok {
				t.Error("expected no handoff detection")
			}
		})
	}
}

func TestDetectHandoffInResults(t *testing.T) {
	results := []string{
		"regular tool output",
		`{"some":"json"}`,
		`{"__handoff__":true,"result":{"target_agent":"next-agent","message":"continue"}}`,
		"more output",
	}

	hr, ok := DetectHandoffInResults(results)
	if !ok {
		t.Fatal("expected handoff in results")
	}
	if hr.TargetAgent != "next-agent" {
		t.Errorf("target = %q, want %q", hr.TargetAgent, "next-agent")
	}
}

func TestDetectHandoffInResults_NoHandoff(t *testing.T) {
	results := []string{
		"regular output",
		`{"data":"value"}`,
	}

	_, ok := DetectHandoffInResults(results)
	if ok {
		t.Error("expected no handoff")
	}
}

func TestNewHandoffSignal(t *testing.T) {
	signal, err := NewHandoffSignal("target", map[string]string{"k": "v"}, "msg")
	if err != nil {
		t.Fatal(err)
	}

	// Should be detectable.
	hr, ok := DetectHandoff(signal)
	if !ok {
		t.Fatal("signal should be detectable")
	}
	if hr.TargetAgent != "target" {
		t.Errorf("target = %q, want %q", hr.TargetAgent, "target")
	}
	if hr.ContextVars["k"] != "v" {
		t.Errorf("context_vars[k] = %q, want %q", hr.ContextVars["k"], "v")
	}
	if hr.Message != "msg" {
		t.Errorf("message = %q, want %q", hr.Message, "msg")
	}
}

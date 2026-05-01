package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolSearch_ReturnsFullSchemas(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterToolSearchHandler(r)

	spec, ok := r.Get("ToolSearch")
	if !ok {
		t.Fatal("ToolSearch not registered")
	}

	input, _ := json.Marshal(map[string]any{
		"query":       "select:copy_file",
		"max_results": 1,
	})

	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("ToolSearch handler error: %v", err)
	}

	// Should contain <functions> block.
	if !strings.Contains(result, "<functions>") {
		t.Error("result should contain <functions> block")
	}
	if !strings.Contains(result, "<function>") {
		t.Error("result should contain <function> tags")
	}

	// Should contain full schema (parameters key).
	if !strings.Contains(result, `"parameters"`) {
		t.Error("result should contain parameters (full schema)")
	}
	if !strings.Contains(result, `"name":"copy_file"`) {
		t.Error("result should contain the tool name")
	}
}

func TestToolSearch_NoResults(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterToolSearchHandler(r)

	spec, _ := r.Get("ToolSearch")
	input, _ := json.Marshal(map[string]any{"query": "zzzznonexistent"})

	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No matching") {
		t.Errorf("expected 'No matching' message, got: %s", result)
	}
}

func TestToolSearch_AlwaysAvailable(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	spec, ok := r.Get("ToolSearch")
	if !ok {
		t.Fatal("ToolSearch not found")
	}
	if !spec.AlwaysAvailable {
		t.Error("ToolSearch should be AlwaysAvailable")
	}

	skillSpec, ok := r.Get("Skill")
	if !ok {
		t.Fatal("Skill not found")
	}
	if !skillSpec.AlwaysAvailable {
		t.Error("Skill should be AlwaysAvailable")
	}
}

func TestAlwaysAvailable_CoreCount(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	always := r.AlwaysAvailable()
	// Should be 9: bash, read_file, write_file, edit_file, glob_search, grep_search, Skill, ToolSearch, Agent
	if len(always) != 9 {
		names := make([]string, len(always))
		for i, s := range always {
			names[i] = s.Name
		}
		t.Errorf("expected 9 always-available tools, got %d: %v", len(always), names)
	}
}

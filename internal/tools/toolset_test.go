package tools

import (
	"testing"
)

func TestResolveBasicToolset(t *testing.T) {
	r := NewToolsetRegistry()
	tools := r.Resolve("core")
	expected := []string{"bash", "edit_file", "glob_search", "grep_search", "read_file", "write_file"}
	if len(tools) != len(expected) {
		t.Fatalf("expected %d tools, got %d: %v", len(expected), len(tools), tools)
	}
	for i, name := range expected {
		if tools[i] != name {
			t.Errorf("tools[%d] = %q, want %q", i, tools[i], name)
		}
	}
}

func TestResolveCompositeToolset(t *testing.T) {
	r := NewToolsetRegistry()
	tools := r.Resolve("research")
	// research = read_file + web(WebFetch, WebSearch) + search(grep_search, glob_search, ToolSearch)
	want := map[string]bool{
		"read_file":   true,
		"WebFetch":    true,
		"WebSearch":   true,
		"grep_search": true,
		"glob_search": true,
		"ToolSearch":  true,
	}
	if len(tools) != len(want) {
		t.Fatalf("expected %d tools, got %d: %v", len(want), len(tools), tools)
	}
	for _, name := range tools {
		if !want[name] {
			t.Errorf("unexpected tool %q in resolved set", name)
		}
	}
}

func TestResolveCyclePrevention(t *testing.T) {
	r := NewToolsetRegistry()
	// Create a cycle: a -> b -> a
	r.Register(&Toolset{
		Name:     "cycle_a",
		Tools:    []string{"tool_a"},
		Includes: []string{"cycle_b"},
	})
	r.Register(&Toolset{
		Name:     "cycle_b",
		Tools:    []string{"tool_b"},
		Includes: []string{"cycle_a"},
	})
	tools := r.Resolve("cycle_a")
	want := map[string]bool{"tool_a": true, "tool_b": true}
	if len(tools) != len(want) {
		t.Fatalf("expected %d tools, got %d: %v", len(want), len(tools), tools)
	}
	for _, name := range tools {
		if !want[name] {
			t.Errorf("unexpected tool %q", name)
		}
	}
}

func TestResolveMultiple(t *testing.T) {
	r := NewToolsetRegistry()
	tools := r.ResolveMultiple([]string{"web", "git"})
	want := map[string]bool{
		"WebFetch":   true,
		"WebSearch":  true,
		"git_diff":   true,
		"git_log":    true,
		"git_status": true,
		"git_blame":  true,
	}
	if len(tools) != len(want) {
		t.Fatalf("expected %d tools, got %d: %v", len(want), len(tools), tools)
	}
	for _, name := range tools {
		if !want[name] {
			t.Errorf("unexpected tool %q", name)
		}
	}
}

func TestResolveNonexistent(t *testing.T) {
	r := NewToolsetRegistry()
	tools := r.Resolve("nonexistent")
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools for nonexistent toolset, got %d: %v", len(tools), tools)
	}
}

func TestList(t *testing.T) {
	r := NewToolsetRegistry()
	names := r.List()
	if len(names) == 0 {
		t.Fatal("expected non-empty list of toolset names")
	}
	// Verify sorted
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("list not sorted: %q before %q", names[i-1], names[i])
		}
	}
	// Verify known toolsets are present
	known := map[string]bool{"core": false, "web": false, "research": false, "full_stack": false}
	for _, name := range names {
		if _, ok := known[name]; ok {
			known[name] = true
		}
	}
	for name, found := range known {
		if !found {
			t.Errorf("expected toolset %q in list", name)
		}
	}
}

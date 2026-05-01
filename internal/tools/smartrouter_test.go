package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

func TestSmartRouter_NoVectorStore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// SmartRouter without a vector store should return empty (no crash).
	sr := NewSmartRouter(nil, nil)

	r := NewRegistry()
	RegisterBuiltins(r)

	result := sr.SelectTools(context.Background(), r, "find all references to HandleAuth", 5)
	if len(result) != 0 {
		t.Errorf("expected empty result without vector store, got: %v", result)
	}
}

func TestSmartRouter_WithPreferences(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Create a stats provider that reports tool usage.
	statsProvider := func(_ context.Context) ([]ToolUsageStats, error) {
		return []ToolUsageStats{
			{Name: "find_references", CallCount: 20, SuccessRate: 0.95},
			{Name: "ast_search", CallCount: 15, SuccessRate: 0.90},
			{Name: "grep_search", CallCount: 50, SuccessRate: 0.99},
			{Name: "read_file", CallCount: 100, SuccessRate: 0.98},  // always-available, should be filtered
			{Name: "some_bad_tool", CallCount: 2, SuccessRate: 0.3}, // low usage + low success, should be excluded
		}, nil
	}

	sr := NewSmartRouter(nil, statsProvider)

	r := NewRegistry()
	RegisterBuiltins(r)
	// Register some deferred tools for the router to find.
	_ = r.Register(&ToolSpec{
		Name:         "find_references",
		Description:  "Find references",
		RequiredMode: permission.ReadOnly,
		Source:       SourceBuiltin,
	})
	_ = r.Register(&ToolSpec{
		Name:         "ast_search",
		Description:  "AST search",
		RequiredMode: permission.ReadOnly,
		Source:       SourceBuiltin,
	})

	result := sr.SelectTools(context.Background(), r, "anything", 5)

	// Should include preference-boosted tools (find_references, ast_search)
	// but NOT always-available tools (read_file, grep_search).
	foundFindRef := false
	foundReadFile := false
	for _, name := range result {
		if name == "find_references" {
			foundFindRef = true
		}
		if name == "read_file" {
			foundReadFile = true
		}
	}
	if !foundFindRef {
		t.Error("expected find_references to be selected via preference boost")
	}
	if foundReadFile {
		t.Error("always-available tools should be filtered out")
	}
}

func TestSmartRouter_PreferenceFiltering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	statsProvider := func(_ context.Context) ([]ToolUsageStats, error) {
		return []ToolUsageStats{
			{Name: "UpdatePlan", CallCount: 10, SuccessRate: 0.9},
			{Name: "nonexistent_tool", CallCount: 50, SuccessRate: 1.0}, // not in registry
			{Name: "low_usage_tool", CallCount: 1, SuccessRate: 1.0},    // too few calls
		}, nil
	}

	sr := NewSmartRouter(nil, statsProvider)

	r := NewRegistry()
	RegisterBuiltins(r)

	result := sr.SelectTools(context.Background(), r, "plan the work", 5)

	// Only UpdatePlan should appear (nonexistent and low-usage filtered).
	for _, name := range result {
		if name == "nonexistent_tool" {
			t.Error("nonexistent tools should be filtered")
		}
		if name == "low_usage_tool" {
			t.Error("low-usage tools should be filtered")
		}
	}
}

func TestSmartRouter_ExplicitPreference(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	statsProvider := func(_ context.Context) ([]ToolUsageStats, error) {
		return []ToolUsageStats{
			{Name: "view_diff", CallCount: 3, SuccessRate: 0.8, IsPreferred: true},
		}, nil
	}

	sr := NewSmartRouter(nil, statsProvider)

	r := NewRegistry()
	RegisterBuiltins(r)

	result := sr.SelectTools(context.Background(), r, "show changes", 5)

	found := false
	for _, name := range result {
		if name == "view_diff" {
			found = true
		}
	}
	if !found {
		t.Error("explicitly preferred tool should be selected")
	}
}

func TestFormatToolSuggestion(t *testing.T) {
	msg := FormatToolSuggestion("bash", []string{"grep_search", "find_references"}, "tree-sitter provides accurate symbol analysis")
	if msg == "" {
		t.Error("expected non-empty suggestion")
	}
	for _, sub := range []string{"bash", "grep_search", "find_references", "tree-sitter"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("suggestion should mention %q, got: %s", sub, msg)
		}
	}
}

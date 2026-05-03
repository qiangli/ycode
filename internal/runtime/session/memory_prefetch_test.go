package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartMemoryPrefetch_Success(t *testing.T) {
	searchFn := func(_ context.Context, query string, maxResults int) ([]MemoryResult, error) {
		return []MemoryResult{
			{Path: "memory1.md", Content: "test content", Age: 2 * time.Hour},
		}, nil
	}

	mp := StartMemoryPrefetch(searchFn, "test query")
	results, err := mp.Collect(time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Path != "memory1.md" {
		t.Errorf("expected memory1.md, got %s", results[0].Path)
	}
}

func TestStartMemoryPrefetch_Error(t *testing.T) {
	searchFn := func(_ context.Context, _ string, _ int) ([]MemoryResult, error) {
		return nil, errors.New("search failed")
	}

	mp := StartMemoryPrefetch(searchFn, "test")
	_, err := mp.Collect(time.Second)
	if err == nil {
		t.Error("expected error")
	}
}

func TestStartMemoryPrefetch_Async(t *testing.T) {
	searchFn := func(_ context.Context, _ string, _ int) ([]MemoryResult, error) {
		time.Sleep(50 * time.Millisecond)
		return []MemoryResult{{Path: "async.md"}}, nil
	}

	mp := StartMemoryPrefetch(searchFn, "test")

	// Should complete within reasonable time.
	select {
	case <-mp.Done():
		// OK.
	case <-time.After(2 * time.Second):
		t.Fatal("prefetch should complete within 2s")
	}

	results, err := mp.Collect(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestDeduplicateMemories(t *testing.T) {
	results := []MemoryResult{
		{Path: "a.md"},
		{Path: "b.md"},
		{Path: "c.md"},
	}

	surfaced := map[string]bool{"b.md": true}
	filtered := DeduplicateMemories(results, surfaced)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 after dedup, got %d", len(filtered))
	}
	for _, r := range filtered {
		if r.Path == "b.md" {
			t.Error("b.md should be filtered out")
		}
	}
}

func TestDeduplicateMemories_EmptySurfaced(t *testing.T) {
	results := []MemoryResult{{Path: "a.md"}, {Path: "b.md"}}
	filtered := DeduplicateMemories(results, nil)
	if len(filtered) != 2 {
		t.Errorf("expected 2, got %d", len(filtered))
	}
}

func TestFormatMemoryForInjection_Fresh(t *testing.T) {
	m := MemoryResult{
		Path:    "user_role.md",
		Content: "User is a backend engineer",
		Age:     2 * time.Hour,
	}

	formatted := FormatMemoryForInjection(m)
	if formatted == "" {
		t.Fatal("should not be empty")
	}
	// Fresh memories should not have staleness note.
	if contains(formatted, "outdated") {
		t.Error("fresh memory should not have staleness caveat")
	}
}

func TestFormatMemoryForInjection_Stale(t *testing.T) {
	m := MemoryResult{
		Path:    "project_plan.md",
		Content: "Sprint ends Friday",
		Age:     72 * time.Hour, // 3 days old
	}

	formatted := FormatMemoryForInjection(m)
	if !contains(formatted, "outdated") {
		t.Error("stale memory should have staleness caveat")
	}
	if !contains(formatted, "3 days ago") {
		t.Errorf("should mention age, got: %s", formatted)
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		days int
		want string
	}{
		{1, "1 day ago"},
		{5, "5 days ago"},
		{35, "1 month ago"},
		{90, "3 months ago"},
		{400, "1 year ago"},
	}

	for _, tt := range tests {
		got := formatAge(tt.days)
		if got != tt.want {
			t.Errorf("formatAge(%d) = %q, want %q", tt.days, got, tt.want)
		}
	}
}

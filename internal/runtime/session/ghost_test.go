package session

import (
	"testing"
	"time"
)

func TestGhostSnapshot_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	snap := &GhostSnapshot{
		Timestamp:       time.Now(),
		MessageCount:    42,
		EstimatedTokens: 95000,
		Summary:         "Working on auth module refactoring",
		CompactedIDs:    []string{"msg-1", "msg-2", "msg-3"},
		KeyFiles:        []string{"internal/auth.go", "internal/auth_test.go"},
		ActiveTopic:     "auth refactoring",
	}

	if err := SaveGhostSnapshot(dir, snap); err != nil {
		t.Fatalf("save ghost: %v", err)
	}

	loaded, err := LoadLatestGhost(dir)
	if err != nil {
		t.Fatalf("load ghost: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil ghost")
	}

	if loaded.MessageCount != 42 {
		t.Errorf("expected message count 42, got %d", loaded.MessageCount)
	}
	if loaded.Summary != "Working on auth module refactoring" {
		t.Errorf("unexpected summary: %s", loaded.Summary)
	}
	if len(loaded.CompactedIDs) != 3 {
		t.Errorf("expected 3 compacted IDs, got %d", len(loaded.CompactedIDs))
	}
}

func TestGhostSnapshot_ListOrdering(t *testing.T) {
	dir := t.TempDir()

	// Save two snapshots with different timestamps.
	snap1 := &GhostSnapshot{
		Timestamp:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		MessageCount: 10,
		Summary:      "first",
	}
	snap2 := &GhostSnapshot{
		Timestamp:    time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		MessageCount: 20,
		Summary:      "second",
	}

	if err := SaveGhostSnapshot(dir, snap1); err != nil {
		t.Fatal(err)
	}
	if err := SaveGhostSnapshot(dir, snap2); err != nil {
		t.Fatal(err)
	}

	ghosts, err := ListGhosts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ghosts) != 2 {
		t.Fatalf("expected 2 ghosts, got %d", len(ghosts))
	}

	// Latest should be the second one.
	loaded, err := LoadLatestGhost(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Summary != "second" {
		t.Errorf("expected latest to be 'second', got %q", loaded.Summary)
	}
}

func TestGhostSnapshot_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	loaded, err := LoadLatestGhost(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded != nil {
		t.Error("expected nil ghost for empty dir")
	}
}

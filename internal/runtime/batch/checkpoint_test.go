package batch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpoint_EmptyPath(t *testing.T) {
	cp, err := NewCheckpoint("")
	if err != nil {
		t.Fatalf("NewCheckpoint: %v", err)
	}
	if cp.IsCompleted("x") {
		t.Error("empty checkpoint should have no completed IDs")
	}
	// Save with empty path should be a no-op.
	if err := cp.Save(); err != nil {
		t.Errorf("Save with empty path: %v", err)
	}
}

func TestCheckpoint_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cp.json")

	cp, err := NewCheckpoint(path)
	if err != nil {
		t.Fatalf("NewCheckpoint: %v", err)
	}
	if len(cp.CompletedIDs) != 0 {
		t.Errorf("expected empty completed IDs, got %d", len(cp.CompletedIDs))
	}
}

func TestCheckpoint_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cp.json")

	cp, err := NewCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := cp.MarkCompleted("id-1"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if err := cp.MarkCompleted("id-2"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	// Verify file was written.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("checkpoint file not created: %v", err)
	}

	// Load from same path.
	cp2, err := NewCheckpoint(path)
	if err != nil {
		t.Fatalf("reload checkpoint: %v", err)
	}
	if !cp2.IsCompleted("id-1") {
		t.Error("id-1 should be completed after reload")
	}
	if !cp2.IsCompleted("id-2") {
		t.Error("id-2 should be completed after reload")
	}
	if cp2.IsCompleted("id-3") {
		t.Error("id-3 should not be completed")
	}
}

func TestCheckpoint_MarkCompleted(t *testing.T) {
	cp, err := NewCheckpoint("")
	if err != nil {
		t.Fatal(err)
	}

	if cp.IsCompleted("a") {
		t.Error("should not be completed before marking")
	}

	if err := cp.MarkCompleted("a"); err != nil {
		t.Fatal(err)
	}

	if !cp.IsCompleted("a") {
		t.Error("should be completed after marking")
	}
}

func TestCheckpoint_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := NewCheckpoint(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON checkpoint")
	}
}

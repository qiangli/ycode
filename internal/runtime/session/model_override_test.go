package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataStore_SetAndGetModelOverride(t *testing.T) {
	dir := t.TempDir()

	store := NewMetadataStore()

	// No override initially.
	if m := store.ModelOverride(dir); m != "" {
		t.Errorf("expected empty, got %q", m)
	}

	// Set override.
	if err := store.SetModelOverride(dir, "claude-haiku"); err != nil {
		t.Fatal(err)
	}
	if m := store.ModelOverride(dir); m != "claude-haiku" {
		t.Errorf("expected claude-haiku, got %q", m)
	}

	// Verify persisted to disk.
	path := filepath.Join(dir, "session_meta.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("metadata file should exist: %v", err)
	}

	// Clear override.
	if err := store.ClearModelOverride(dir); err != nil {
		t.Fatal(err)
	}
	if m := store.ModelOverride(dir); m != "" {
		t.Errorf("expected empty after clear, got %q", m)
	}
}

func TestMetadataStore_PersistenceAcrossInstances(t *testing.T) {
	dir := t.TempDir()

	// First store instance writes.
	store1 := NewMetadataStore()
	store1.SetModelOverride(dir, "gpt-4o")

	// Second store instance reads from disk.
	store2 := NewMetadataStore()
	if m := store2.ModelOverride(dir); m != "gpt-4o" {
		t.Errorf("expected gpt-4o from disk, got %q", m)
	}
}

func TestMetadataStore_CorruptFile(t *testing.T) {
	dir := t.TempDir()

	// Write garbage to the metadata file.
	path := filepath.Join(dir, "session_meta.json")
	os.WriteFile(path, []byte("not json"), 0o644)

	store := NewMetadataStore()
	// Should return empty metadata, not error.
	if m := store.ModelOverride(dir); m != "" {
		t.Errorf("expected empty for corrupt file, got %q", m)
	}
}

func TestMetadataStore_Get(t *testing.T) {
	dir := t.TempDir()
	store := NewMetadataStore()

	m, err := store.Get(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ModelOverride != "" {
		t.Errorf("expected empty override, got %q", m.ModelOverride)
	}

	// Set and get again.
	store.SetModelOverride(dir, "test-model")
	m, err = store.Get(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ModelOverride != "test-model" {
		t.Errorf("expected test-model, got %q", m.ModelOverride)
	}
}

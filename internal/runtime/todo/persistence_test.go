package todo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadBoardRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todos.json")

	b := NewBoard()
	t1 := b.Create("Task 1", "desc1", "", 1)
	t2 := b.Create("Task 2", "desc2", t1.ID, 2)
	b.Update(t1.ID, StatusDone)
	b.Assign(t2.ID, "agent-1")
	b.AddDependency(t2.ID, t1.ID)

	if err := b.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadBoard(path)
	if err != nil {
		t.Fatalf("LoadBoard: %v", err)
	}

	if loaded.Len() != 2 {
		t.Fatalf("loaded len = %d, want 2", loaded.Len())
	}

	got1, ok := loaded.Get(t1.ID)
	if !ok {
		t.Fatalf("task %q not found", t1.ID)
	}
	if got1.Status != StatusDone {
		t.Fatalf("status = %q, want done", got1.Status)
	}
	if got1.Title != "Task 1" {
		t.Fatalf("title = %q", got1.Title)
	}

	got2, ok := loaded.Get(t2.ID)
	if !ok {
		t.Fatalf("task %q not found", t2.ID)
	}
	if got2.AssignedTo != "agent-1" {
		t.Fatalf("assigned_to = %q", got2.AssignedTo)
	}
	if got2.ParentID != t1.ID {
		t.Fatalf("parent_id = %q, want %q", got2.ParentID, t1.ID)
	}
	if len(got2.Dependencies) != 1 || got2.Dependencies[0] != t1.ID {
		t.Fatalf("dependencies = %v", got2.Dependencies)
	}
}

func TestLoadBoardNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")

	b, err := LoadBoard(path)
	if err != nil {
		t.Fatalf("LoadBoard: %v", err)
	}
	if b.Len() != 0 {
		t.Fatalf("len = %d, want 0", b.Len())
	}
}

func TestLoadBoardCorrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadBoard(path)
	if err == nil {
		t.Fatal("expected error for corrupted file")
	}
}

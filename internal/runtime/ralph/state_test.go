package ralph

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &State{
		Iteration:       5,
		Status:          "running",
		LastScore:       0.85,
		LastOutput:      "output text",
		LastCheckOutput: "check ok",
		LastError:       "",
		Commits:         []string{"commit1", "commit2"},
	}

	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.Iteration != 5 {
		t.Fatalf("iteration = %d, want 5", loaded.Iteration)
	}
	if loaded.Status != "running" {
		t.Fatalf("status = %q, want running", loaded.Status)
	}
	if loaded.LastScore != 0.85 {
		t.Fatalf("score = %f, want 0.85", loaded.LastScore)
	}
	if loaded.LastOutput != "output text" {
		t.Fatalf("output = %q", loaded.LastOutput)
	}
	if loaded.LastCheckOutput != "check ok" {
		t.Fatalf("check output = %q", loaded.LastCheckOutput)
	}
	if len(loaded.Commits) != 2 || loaded.Commits[0] != "commit1" {
		t.Fatalf("commits = %v", loaded.Commits)
	}
}

func TestLoadStateNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does_not_exist.json")

	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if s.Status != "pending" {
		t.Fatalf("status = %q, want pending", s.Status)
	}
	if s.Iteration != 0 {
		t.Fatalf("iteration = %d, want 0", s.Iteration)
	}
}

func TestLoadStateCorrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	if err := os.WriteFile(path, []byte("{invalid json!!!"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadState(path)
	if err == nil {
		t.Fatal("expected error for corrupted file")
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "deep", "state.json")

	s := NewState()
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
}

func TestNewState(t *testing.T) {
	s := NewState()
	if s.Status != "pending" {
		t.Fatalf("status = %q, want pending", s.Status)
	}
	if s.Iteration != 0 {
		t.Fatalf("iteration = %d, want 0", s.Iteration)
	}
}

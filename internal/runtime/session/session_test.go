package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewWithID(t *testing.T) {
	dir := t.TempDir()
	id := "test-instance-42"

	sess, err := NewWithID(dir, id)
	if err != nil {
		t.Fatalf("NewWithID: %v", err)
	}
	if sess.ID != id {
		t.Fatalf("got ID %q, want %q", sess.ID, id)
	}
	expectedDir := filepath.Join(dir, id)
	if sess.Dir != expectedDir {
		t.Fatalf("got Dir %q, want %q", sess.Dir, expectedDir)
	}
	// Directory should exist.
	info, err := os.Stat(expectedDir)
	if err != nil {
		t.Fatalf("session dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("session dir is not a directory")
	}
}

func TestNew(t *testing.T) {
	dir := t.TempDir()

	sess, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if _, err := os.Stat(sess.Dir); err != nil {
		t.Fatalf("session dir not created: %v", err)
	}
}

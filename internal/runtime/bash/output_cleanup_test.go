package bash

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanOldOutputFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	dir := t.TempDir()

	// Create an old file (modify time set to 8 days ago).
	oldFile := filepath.Join(dir, "old_output.txt")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Create a recent file.
	recentFile := filepath.Join(dir, "recent_output.txt")
	if err := os.WriteFile(recentFile, []byte("recent"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run cleanup.
	cleanOldOutputFiles(dir, nil)

	// Old file should be removed.
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("expected old file to be removed")
	}

	// Recent file should remain.
	if _, err := os.Stat(recentFile); err != nil {
		t.Error("expected recent file to remain")
	}
}

func TestCleanOldOutputFiles_NonexistentDir(t *testing.T) {
	// Should not panic on nonexistent directory.
	cleanOldOutputFiles("/nonexistent/path", nil)
}

func TestCleanOldOutputFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	// Should not panic on empty directory.
	cleanOldOutputFiles(dir, nil)
}

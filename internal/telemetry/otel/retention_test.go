package otel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupOldFiles(t *testing.T) {
	dir := t.TempDir()

	// Create files with different modification times.
	oldFile := filepath.Join(dir, "traces-2020-01-01.jsonl")
	newFile := filepath.Join(dir, "traces-2099-12-31.jsonl")

	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Set old file's mtime to the past.
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(newFile, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Cleanup with 3-day retention.
	CleanupOldFiles(dir, 3*24*time.Hour)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old file should have been removed")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("new file should still exist: %v", err)
	}
}

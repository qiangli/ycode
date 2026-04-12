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

func TestRunCleanupPerInstance(t *testing.T) {
	dataDir := t.TempDir()
	maxAge := 3 * 24 * time.Hour
	oldTime := time.Now().Add(-10 * 24 * time.Hour)

	// Create per-instance directory with old and new files.
	instDir := filepath.Join(dataDir, "instances", "uuid-abc")
	for _, sub := range []string{"logs", "traces", "metrics"} {
		dir := filepath.Join(instDir, sub)
		os.MkdirAll(dir, 0o755)

		oldFile := filepath.Join(dir, sub+"-2020-01-01.jsonl")
		newFile := filepath.Join(dir, sub+"-2099-12-31.jsonl")

		os.WriteFile(oldFile, []byte("old"), 0o644)
		os.Chtimes(oldFile, oldTime, oldTime)
		os.WriteFile(newFile, []byte("new"), 0o644)
	}

	runCleanup(dataDir, maxAge)

	// Old files should be removed, new files should remain.
	for _, sub := range []string{"logs", "traces", "metrics"} {
		dir := filepath.Join(instDir, sub)
		oldFile := filepath.Join(dir, sub+"-2020-01-01.jsonl")
		newFile := filepath.Join(dir, sub+"-2099-12-31.jsonl")

		if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
			t.Errorf("%s: old file should be removed", sub)
		}
		if _, err := os.Stat(newFile); err != nil {
			t.Errorf("%s: new file should remain: %v", sub, err)
		}
	}

	// Instance dir should still exist (has new files).
	if _, err := os.Stat(instDir); err != nil {
		t.Fatalf("instance dir should still exist: %v", err)
	}
}

func TestRunCleanupRemovesEmptyInstanceDir(t *testing.T) {
	dataDir := t.TempDir()
	maxAge := 3 * 24 * time.Hour
	oldTime := time.Now().Add(-10 * 24 * time.Hour)

	// Create per-instance dir with only old files.
	instDir := filepath.Join(dataDir, "instances", "uuid-expired")
	for _, sub := range []string{"logs", "traces", "metrics"} {
		dir := filepath.Join(instDir, sub)
		os.MkdirAll(dir, 0o755)

		oldFile := filepath.Join(dir, sub+"-2020-01-01.jsonl")
		os.WriteFile(oldFile, []byte("old"), 0o644)
		os.Chtimes(oldFile, oldTime, oldTime)
	}

	runCleanup(dataDir, maxAge)

	// All files removed, instance dir should also be removed.
	if _, err := os.Stat(instDir); !os.IsNotExist(err) {
		t.Fatalf("empty instance dir should be removed")
	}
}

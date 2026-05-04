package bash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutputSpiller_SmallOutput(t *testing.T) {
	cfg := DefaultSpillConfig()
	cfg.Dir = t.TempDir()
	spiller := NewOutputSpiller(cfg)

	output := []byte("hello world")
	result, err := spiller.Spill(output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Spilled {
		t.Error("small output should not be spilled")
	}
	if result.Preview != "hello world" {
		t.Errorf("preview = %q, want %q", result.Preview, "hello world")
	}
	if result.FullSize != 11 {
		t.Errorf("fullSize = %d, want 11", result.FullSize)
	}
}

func TestOutputSpiller_LargeOutput(t *testing.T) {
	cfg := DefaultSpillConfig()
	cfg.Dir = t.TempDir()
	cfg.Threshold = 100 // low threshold for testing
	cfg.PreviewLines = 3
	spiller := NewOutputSpiller(cfg)

	// Generate output larger than threshold.
	var sb strings.Builder
	for range 20 {
		sb.WriteString("line of output that is fairly long\n")
	}
	output := []byte(sb.String())

	result, err := spiller.Spill(output)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Spilled {
		t.Error("large output should be spilled")
	}
	if result.FilePath == "" {
		t.Error("expected file path for spilled output")
	}
	if result.FullSize != len(output) {
		t.Errorf("fullSize = %d, want %d", result.FullSize, len(output))
	}

	// Verify file exists and has correct content.
	data, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("reading spill file: %v", err)
	}
	if len(data) != len(output) {
		t.Errorf("spill file size = %d, want %d", len(data), len(output))
	}

	// Preview should be truncated.
	if !strings.Contains(result.Preview, "truncated") {
		t.Error("expected truncation notice in preview")
	}
}

func TestOutputSpiller_Cleanup(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultSpillConfig()
	cfg.Dir = dir
	cfg.Retention = 1 * time.Second
	spiller := NewOutputSpiller(cfg)

	// Create an old file.
	oldFile := filepath.Join(dir, "tool_old.txt")
	if err := os.WriteFile(oldFile, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Backdate the file.
	oldTime := time.Now().Add(-2 * time.Second)
	os.Chtimes(oldFile, oldTime, oldTime)

	// Create a recent file.
	recentFile := filepath.Join(dir, "tool_recent.txt")
	if err := os.WriteFile(recentFile, []byte("recent"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := spiller.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	// Old file should be gone.
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("expected old file to be removed")
	}
	// Recent file should remain.
	if _, err := os.Stat(recentFile); err != nil {
		t.Error("expected recent file to remain")
	}
}

func TestOutputSpiller_CleanupEmptyDir(t *testing.T) {
	cfg := DefaultSpillConfig()
	cfg.Dir = filepath.Join(t.TempDir(), "nonexistent")
	spiller := NewOutputSpiller(cfg)

	removed, err := spiller.Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}

func TestTailLines(t *testing.T) {
	text := "line1\nline2\nline3\nline4\nline5\n"

	result := tailLines(text, 2)
	if !strings.Contains(result, "line4") || !strings.Contains(result, "line5") {
		t.Errorf("expected last 2 lines, got: %q", result)
	}

	// Small text should return as-is.
	small := "abc"
	if tailLines(small, 10) != small {
		t.Error("small text should return unchanged")
	}
}

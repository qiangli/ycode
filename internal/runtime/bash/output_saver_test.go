package bash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLargeOutput_Small(t *testing.T) {
	output := "small output"
	result := SaveLargeOutput(output, "bash", "")
	if result != output {
		t.Error("small output should be returned unchanged")
	}
}

func TestSaveLargeOutput_LargeNoDisk(t *testing.T) {
	output := strings.Repeat("line of output\n", 5000)
	result := SaveLargeOutput(output, "bash", "")
	if len(result) >= len(output) {
		t.Error("large output without disk should be truncated")
	}
	if !strings.Contains(result, "lines omitted") {
		t.Error("should contain omission marker")
	}
}

func TestSaveLargeOutput_LargeWithDisk(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping disk test")
	}

	dir := t.TempDir()
	output := strings.Repeat("line of output\n", 5000)

	result := SaveLargeOutput(output, "bash", dir)
	if !strings.Contains(result, "full output saved to") {
		t.Error("should reference saved file")
	}
	if !strings.Contains(result, "Use Read") {
		t.Error("should include read instruction")
	}

	// Verify file was saved.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 saved file, got %d", len(entries))
	}

	savedPath := filepath.Join(dir, entries[0].Name())
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(output) {
		t.Errorf("saved file size mismatch: got %d, want %d", len(data), len(output))
	}
}

func TestTruncateLargeOutput(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line content"
	}
	output := strings.Join(lines, "\n")

	result := truncateLargeOutput(output)
	if !strings.Contains(result, "lines omitted") {
		t.Error("should contain omission marker")
	}
}

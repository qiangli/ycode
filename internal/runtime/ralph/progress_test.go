package ralph

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProgressLogAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress.txt")
	log := NewProgressLog(path)

	if err := log.Append(1, "S1", "implement feature", "success", "use interfaces"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := log.Append(2, "S2", "fix bug", "failed", ""); err != nil {
		t.Fatalf("Append: %v", err)
	}

	content, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(content, "Iteration 1") {
		t.Fatal("missing iteration 1")
	}
	if !strings.Contains(content, "Iteration 2") {
		t.Fatal("missing iteration 2")
	}
	if !strings.Contains(content, "Story: S1") {
		t.Fatal("missing story S1")
	}
	if !strings.Contains(content, "Learnings: use interfaces") {
		t.Fatal("missing learnings")
	}
	// Iteration 2 has no learnings.
	if strings.Contains(content, "Learnings: \n") {
		t.Fatal("should not have empty learnings line")
	}
}

func TestProgressLogReadNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope.txt")
	log := NewProgressLog(path)

	content, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty, got %q", content)
	}
}

func TestProgressLogReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress.txt")
	log := NewProgressLog(path)

	if err := log.Append(1, "S1", "work", "ok", "learned stuff"); err != nil {
		t.Fatal(err)
	}

	if err := log.Reset("New run started"); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	content, err := log.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(content, "# Progress Log") {
		t.Fatal("missing header after reset")
	}
	if strings.Contains(content, "Iteration 1") {
		t.Fatal("old content should be gone after reset")
	}
	if !strings.Contains(content, "New run started") {
		t.Fatal("missing reset header text")
	}
}

func TestExtractLearnings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress.txt")
	log := NewProgressLog(path)

	if err := log.Append(1, "S1", "work", "ok", "first learning"); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(2, "S1", "more work", "ok", "second learning"); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(3, "S2", "other", "fail", ""); err != nil {
		t.Fatal(err)
	}

	learnings, err := log.ExtractLearnings()
	if err != nil {
		t.Fatalf("ExtractLearnings: %v", err)
	}
	if len(learnings) != 2 {
		t.Fatalf("learnings = %d, want 2", len(learnings))
	}
	if learnings[0] != "first learning" {
		t.Fatalf("first = %q", learnings[0])
	}
	if learnings[1] != "second learning" {
		t.Fatalf("second = %q", learnings[1])
	}
}

func TestExtractLearningsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope.txt")
	log := NewProgressLog(path)

	learnings, err := log.ExtractLearnings()
	if err != nil {
		t.Fatal(err)
	}
	if len(learnings) != 0 {
		t.Fatalf("expected no learnings, got %d", len(learnings))
	}
}

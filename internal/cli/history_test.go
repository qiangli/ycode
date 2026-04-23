package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptHistory_InMemory(t *testing.T) {
	h := newPromptHistory("")

	// Empty history — Up returns nothing.
	if _, ok := h.Up(""); ok {
		t.Fatal("Up should return false on empty history")
	}
	if _, ok := h.Down(); ok {
		t.Fatal("Down should return false when not browsing")
	}

	h.Append("first")
	h.Append("second")
	h.Append("third")

	// Up from newest to oldest.
	val, ok := h.Up("current draft")
	if !ok || val != "third" {
		t.Fatalf("expected 'third', got %q (ok=%v)", val, ok)
	}
	val, ok = h.Up("")
	if !ok || val != "second" {
		t.Fatalf("expected 'second', got %q", val)
	}
	val, ok = h.Up("")
	if !ok || val != "first" {
		t.Fatalf("expected 'first', got %q", val)
	}
	// At oldest — no further movement.
	if _, ok := h.Up(""); ok {
		t.Fatal("Up should return false at oldest entry")
	}

	// Down back to newest, then restore draft.
	val, ok = h.Down()
	if !ok || val != "second" {
		t.Fatalf("expected 'second', got %q", val)
	}
	val, ok = h.Down()
	if !ok || val != "third" {
		t.Fatalf("expected 'third', got %q", val)
	}
	val, ok = h.Down()
	if !ok || val != "current draft" {
		t.Fatalf("expected draft 'current draft', got %q", val)
	}
	// Past end — not browsing.
	if _, ok := h.Down(); ok {
		t.Fatal("Down should return false after restoring draft")
	}
}

func TestPromptHistory_DuplicateSuppression(t *testing.T) {
	h := newPromptHistory("")
	h.Append("hello")
	h.Append("hello") // consecutive duplicate
	h.Append("world")
	h.Append("hello") // not consecutive — should be kept

	if len(h.entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(h.entries), h.entries)
	}
}

func TestPromptHistory_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Write some history.
	h1 := newPromptHistory(dir)
	h1.Append("alpha")
	h1.Append("beta")
	h1.Append("gamma")

	// Load in a new instance — should see persisted entries.
	h2 := newPromptHistory(dir)
	if len(h2.entries) != 3 {
		t.Fatalf("expected 3 entries after reload, got %d", len(h2.entries))
	}
	if h2.entries[0] != "alpha" || h2.entries[2] != "gamma" {
		t.Fatalf("unexpected entries: %v", h2.entries)
	}

	// Navigate to verify loaded data works.
	val, ok := h2.Up("")
	if !ok || val != "gamma" {
		t.Fatalf("expected 'gamma', got %q", val)
	}
}

func TestPromptHistory_Trimming(t *testing.T) {
	dir := t.TempDir()
	h := newPromptHistory(dir)

	// Add more than maxHistoryEntries.
	for i := 0; i < maxHistoryEntries+20; i++ {
		h.Append(strings.Repeat("x", i+1)) // unique entries
	}

	if len(h.entries) != maxHistoryEntries {
		t.Fatalf("expected %d entries after trimming, got %d", maxHistoryEntries, len(h.entries))
	}

	// Reload and verify trim persisted.
	h2 := newPromptHistory(dir)
	if len(h2.entries) != maxHistoryEntries {
		t.Fatalf("expected %d entries after reload, got %d", maxHistoryEntries, len(h2.entries))
	}
}

func TestPromptHistory_SelfHealing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, historyFileName)

	// Write a file with some corrupt lines.
	lines := []string{
		`{"input":"good1"}`,
		`{corrupt json`,
		`{"input":"good2"}`,
		`not json at all`,
		`{"input":"good3"}`,
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	h := newPromptHistory(dir)
	if len(h.entries) != 3 {
		t.Fatalf("expected 3 valid entries, got %d: %v", len(h.entries), h.entries)
	}

	// Verify the file was rewritten with only valid entries.
	data, _ := os.ReadFile(path)
	rewritten := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(rewritten) != 3 {
		t.Fatalf("rewritten file should have 3 lines, got %d", len(rewritten))
	}
	for _, line := range rewritten {
		var e historyEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("rewritten line is not valid JSON: %s", line)
		}
	}
}

func TestPromptHistory_EmptyAndWhitespace(t *testing.T) {
	h := newPromptHistory("")
	h.Append("")
	h.Append("   ")
	h.Append("\t\n")
	if len(h.entries) != 0 {
		t.Fatalf("expected 0 entries for blank inputs, got %d", len(h.entries))
	}
}

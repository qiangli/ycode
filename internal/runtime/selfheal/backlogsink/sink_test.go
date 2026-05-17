package backlogsink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/gitserver/backlog"
	"github.com/qiangli/ycode/internal/runtime/selfheal/detector"
)

// TestBacklogSink_WritesValidIssue exercises the end-to-end path
// against a tempdir, then re-loads the entry via the backlog package
// to confirm the frontmatter is parseable by the canonical loader.
// This is the cross-package contract test that catches drift if
// either side's schema ever changes.
func TestBacklogSink_WritesValidIssue(t *testing.T) {
	dir := t.TempDir()
	sink := NewBacklogSink(dir)
	sig := detector.FailureSignal{
		Timestamp:    time.Date(2026, 5, 17, 9, 30, 0, 0, time.UTC),
		Signature:    "abc123def456",
		Category:     detector.CategoryBroken,
		ToolName:     "browser_screenshot",
		Scope:        "ycode.tool.call",
		ErrorMessage: "panic: runtime error",
		Normalized:   "panic: runtime error",
		DurationMs:   42,
		OccurrenceN:  1,
	}
	if err := sink.Record(sig); err != nil {
		t.Fatalf("Record: %v", err)
	}
	wantSlug := "selfheal-abc123def456-browser-screenshot"
	wantPath := filepath.Join(dir, wantSlug+".md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected %s to exist: %v", wantPath, err)
	}

	// Re-load through the canonical backlog loader — anything it can't
	// parse means we'd have produced unreadable backlog entries.
	items, err := backlog.Load(dir)
	if err != nil {
		t.Fatalf("backlog.Load: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("loaded %d items; want 1", len(items))
	}
	it := items[0]
	if it.Slug != wantSlug {
		t.Fatalf("slug = %q; want %q", it.Slug, wantSlug)
	}
	if it.Priority != backlog.PriorityP2 {
		t.Fatalf("priority = %q; want p2 (selfheal default)", it.Priority)
	}
	if it.State != backlog.StateOpen {
		t.Fatalf("state = %q; want open", it.State)
	}
	if !strings.Contains(it.Title, "browser_screenshot") {
		t.Fatalf("title missing tool name: %q", it.Title)
	}
	if !strings.Contains(it.Body, "signature: abc123def456") {
		t.Fatalf("body missing signature: %s", it.Body)
	}
	if !strings.Contains(it.Body, "category: broken") {
		t.Fatalf("body missing category: %s", it.Body)
	}
}

// TestBacklogSink_IdempotentOnReSighting — a signature captured in a
// previous serve must not clobber the existing markdown file (which
// may have operator edits or a worker's state transition).
func TestBacklogSink_IdempotentOnReSighting(t *testing.T) {
	dir := t.TempDir()
	sink := NewBacklogSink(dir)
	sig := detector.FailureSignal{
		Timestamp:   time.Now(),
		Signature:   "dedupedup",
		Category:    detector.CategoryMissing,
		ToolName:    "browser_eval",
		Scope:       "ycode.tool.call",
		Normalized:  "action evaluate not supported",
		OccurrenceN: 1,
	}
	if err := sink.Record(sig); err != nil {
		t.Fatalf("first Record: %v", err)
	}

	// Operator-edit simulation: replace the body so we can detect a
	// clobber. If Record were not idempotent, this sentinel would
	// disappear.
	path := filepath.Join(dir, "selfheal-dedupedup-browser-eval.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	const sentinel = "## human-added section\n"
	modified := append(data, []byte(sentinel)...)
	if err := os.WriteFile(path, modified, 0o600); err != nil {
		t.Fatalf("write modified: %v", err)
	}

	if err := sink.Record(sig); err != nil {
		t.Fatalf("second Record (idempotent): %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if !strings.Contains(string(got), sentinel) {
		t.Fatalf("sink clobbered operator edits; sentinel missing from:\n%s", got)
	}
}

// TestBacklogSink_SlugSanitization — tool names with characters
// outside the kebab alphabet still produce a valid filename.
func TestBacklogSink_SlugSanitization(t *testing.T) {
	dir := t.TempDir()
	sink := NewBacklogSink(dir)
	sig := detector.FailureSignal{
		Timestamp:  time.Now(),
		Signature:  "ffffffffffff",
		Category:   detector.CategoryBroken,
		ToolName:   "weird/tool name with spaces!",
		Normalized: "panic",
	}
	if err := sink.Record(sig); err != nil {
		t.Fatalf("Record: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "selfheal-ffffffffffff-*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("glob = %v err=%v; want 1 match", matches, err)
	}
}

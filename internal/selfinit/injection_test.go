package selfinit

import (
	"strings"
	"testing"
)

func TestSpliceBlock_EmptyExisting(t *testing.T) {
	got := SpliceBlock("", "hello")
	if !strings.Contains(got, BeginMarker) || !strings.Contains(got, EndMarker) {
		t.Fatalf("missing markers: %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("missing body: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("expected trailing newline, got %q", got)
	}
}

func TestSpliceBlock_AppendToBrownfield(t *testing.T) {
	existing := "# My README\n\nUser content here.\n"
	got := SpliceBlock(existing, "ycode reference")
	if !strings.HasPrefix(got, "# My README") {
		t.Errorf("user content lost: %q", got)
	}
	if !strings.Contains(got, BeginMarker) {
		t.Errorf("BEGIN marker missing")
	}
	if !strings.Contains(got, "ycode reference") {
		t.Errorf("body missing")
	}
	// Idempotent re-splice.
	got2 := SpliceBlock(got, "ycode reference")
	if got != got2 {
		t.Errorf("not idempotent\nfirst: %q\nsecond: %q", got, got2)
	}
}

func TestSpliceBlock_ReplaceExistingBlock(t *testing.T) {
	original := SpliceBlock("# README\n", "old body")
	updated := SpliceBlock(original, "new body")
	if strings.Contains(updated, "old body") {
		t.Errorf("old body not removed: %q", updated)
	}
	if !strings.Contains(updated, "new body") {
		t.Errorf("new body missing: %q", updated)
	}
	// Block count: exactly one.
	if c := strings.Count(updated, BeginMarker); c != 1 {
		t.Errorf("expected 1 BEGIN marker, got %d", c)
	}
	if c := strings.Count(updated, EndMarker); c != 1 {
		t.Errorf("expected 1 END marker, got %d", c)
	}
}

func TestHasBlock(t *testing.T) {
	if HasBlock("# nope") {
		t.Errorf("HasBlock false positive")
	}
	if !HasBlock(WrapBlock("body")) {
		t.Errorf("HasBlock false negative")
	}
}


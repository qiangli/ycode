package weaveboard

import (
	"context"
	"errors"
	"testing"
)

func TestBootstrap_StillStubbed(t *testing.T) {
	// Until the CSRF/session flow lands, Bootstrap returns
	// ErrNotYetImplemented so the CLI subverb (`ycode weave
	// init-board`) can surface a clean opt-in-not-wired-yet
	// envelope. When the real impl ships, this test is replaced
	// with the integration test.
	_, err := Bootstrap(context.Background(), Options{
		BaseURL: "http://127.0.0.1:0",
		Owner:   "admin",
		Repo:    "myapp",
	})
	if !errors.Is(err, ErrNotYetImplemented) {
		t.Errorf("expected ErrNotYetImplemented, got %v", err)
	}
}

func TestCanonicalColumns_Order(t *testing.T) {
	got := CanonicalColumns()
	want := []string{"todo", "working", "submitted", "ci_failed", "conflict", "merged", "abandoned"}
	if len(got) != len(want) {
		t.Fatalf("got %d columns, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d: got %q want %q", i, got[i], want[i])
		}
	}
}

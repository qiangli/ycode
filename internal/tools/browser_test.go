package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestBrowserDispatchHook verifies that when a hook is installed,
// every browser_* tool routes through it. This is the seam the
// experimental backends (live / probe / solo) hook into.
func TestBrowserDispatchHook(t *testing.T) {
	t.Cleanup(func() { SetBrowserDispatchHook(nil) })

	var called int
	var seenAction string
	SetBrowserDispatchHook(func(ctx context.Context, action BrowserAction) (*BrowserResult, error) {
		called++
		seenAction = action.Type
		return &BrowserResult{Success: true, Title: "via-hook", URL: action.URL}, nil
	})

	out, err := executeBrowserAction(context.Background(),
		json.RawMessage(`{"url":"https://example.com"}`), "navigate")
	if err != nil {
		t.Fatalf("executeBrowserAction: %v", err)
	}
	if !strings.Contains(out, "via-hook") {
		t.Fatalf("hook output not seen; got %q", out)
	}
	if called != 1 {
		t.Fatalf("hook should be called exactly once; got %d", called)
	}
	if seenAction != "navigate" {
		t.Fatalf("hook saw wrong action type; got %q", seenAction)
	}
}

// TestBrowserNoBackend verifies the friendly error message when no
// hook is installed (stable build, or experimental build without a
// configured browser.mode).
func TestBrowserNoBackend(t *testing.T) {
	t.Cleanup(func() { SetBrowserDispatchHook(nil) })
	SetBrowserDispatchHook(nil)

	out, err := executeBrowserAction(context.Background(),
		json.RawMessage(`{"url":"https://example.com"}`), "navigate")
	if err != nil {
		t.Fatalf("executeBrowserAction: %v", err)
	}
	if !strings.Contains(out, "not available") {
		t.Fatalf("expected not-available message; got %q", out)
	}
}

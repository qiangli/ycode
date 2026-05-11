package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/browser"
	"github.com/qiangli/ycode/pkg/browser/wire"
)

// fakeClient is a test double for browser.Client. The Execute func is
// captured so each test can assert what action it received.
type fakeClient struct {
	exec func(ctx context.Context, action wire.Action) (*wire.Result, error)
}

func (f *fakeClient) Execute(ctx context.Context, action wire.Action) (*wire.Result, error) {
	return f.exec(ctx, action)
}

func (f *fakeClient) Close() error { return nil }

// TestBrowserClientDispatch verifies that when a Client is installed
// on ctx, every browser_* tool routes its wire.Action through it.
// This is the seam the experimental backends (live / probe / solo)
// reach via NewInprocClient.
func TestBrowserClientDispatch(t *testing.T) {
	var called int
	var seenAction string
	client := &fakeClient{
		exec: func(_ context.Context, a wire.Action) (*wire.Result, error) {
			called++
			seenAction = a.Type
			return &wire.Result{Success: true, Title: "via-hook", URL: a.URL}, nil
		},
	}
	ctx := browser.WithClient(context.Background(), client)

	out, err := executeBrowserAction(ctx,
		json.RawMessage(`{"url":"https://example.com"}`), wire.ActionNavigate)
	if err != nil {
		t.Fatalf("executeBrowserAction: %v", err)
	}
	if !strings.Contains(out, "via-hook") {
		t.Fatalf("hook output not seen; got %q", out)
	}
	if called != 1 {
		t.Fatalf("client should be called exactly once; got %d", called)
	}
	if seenAction != wire.ActionNavigate {
		t.Fatalf("client saw wrong action type; got %q", seenAction)
	}
}

// TestBrowserNoBackend verifies the friendly error message when no
// client is installed (stable build, or experimental build without a
// configured browser.mode).
func TestBrowserNoBackend(t *testing.T) {
	out, err := executeBrowserAction(context.Background(),
		json.RawMessage(`{"url":"https://example.com"}`), wire.ActionNavigate)
	if err != nil {
		t.Fatalf("executeBrowserAction: %v", err)
	}
	if !strings.Contains(out, "not available") {
		t.Fatalf("expected not-available message; got %q", out)
	}
}

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

// TestBrowserEvalDispatch verifies that browser_eval routes the
// script through Client.Execute as wire.ActionEvaluate with the
// script preserved verbatim. The backend support for evaluate already
// exists in probe + solo (mcpservers/{probe,solo}/service.go).
func TestBrowserEvalDispatch(t *testing.T) {
	var seen wire.Action
	client := &fakeClient{
		exec: func(_ context.Context, a wire.Action) (*wire.Result, error) {
			seen = a
			return &wire.Result{Success: true, Data: `"page-title"`}, nil
		},
	}
	ctx := browser.WithClient(context.Background(), client)

	reg := NewRegistry()
	registerBrowserEval(reg)
	spec, ok := reg.Get("browser_eval")
	if !ok {
		t.Fatalf("browser_eval not registered")
	}

	out, err := spec.Handler(ctx, json.RawMessage(`{"script":"document.title"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if seen.Type != wire.ActionEvaluate {
		t.Fatalf("action.Type = %q, want %q", seen.Type, wire.ActionEvaluate)
	}
	if seen.Script != "document.title" {
		t.Fatalf("action.Script = %q, want %q", seen.Script, "document.title")
	}
	if !strings.Contains(out, "page-title") {
		t.Fatalf("expected result body in output; got %q", out)
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

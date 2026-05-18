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
// script preserved verbatim. Regression guard for the
// pre-a8a74f3 "action evaluate not supported" bug.
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

// TestBrowserClickFieldsPlumbed ensures the new click fields
// (match_text, scope) round-trip through the dispatcher unchanged.
// This is the seam Ralph's extract-click-by-text strategy relies on.
func TestBrowserClickFieldsPlumbed(t *testing.T) {
	var seen wire.Action
	client := &fakeClient{
		exec: func(_ context.Context, a wire.Action) (*wire.Result, error) {
			seen = a
			return &wire.Result{Success: true}, nil
		},
	}
	ctx := browser.WithClient(context.Background(), client)
	reg := NewRegistry()
	registerBrowserClick(reg)
	spec, _ := reg.Get("browser_click")
	if _, err := spec.Handler(ctx, json.RawMessage(`{"match_text":"Copy","scope":"main"}`)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if seen.Type != wire.ActionClick {
		t.Fatalf("Type = %q", seen.Type)
	}
	if seen.MatchText != "Copy" {
		t.Fatalf("MatchText = %q", seen.MatchText)
	}
	if seen.Scope != "main" {
		t.Fatalf("Scope = %q", seen.Scope)
	}
}

// TestBrowserExtractFieldsPlumbed ensures scope+match_text+limit+offset
// reach the dispatcher.
func TestBrowserExtractFieldsPlumbed(t *testing.T) {
	var seen wire.Action
	client := &fakeClient{
		exec: func(_ context.Context, a wire.Action) (*wire.Result, error) {
			seen = a
			return &wire.Result{Success: true, Total: 42, Truncated: true}, nil
		},
	}
	ctx := browser.WithClient(context.Background(), client)
	reg := NewRegistry()
	registerBrowserExtract(reg)
	spec, _ := reg.Get("browser_extract")
	out, err := spec.Handler(ctx, json.RawMessage(`{"scope":"main","match_text":"Copy","limit":10,"offset":5}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if seen.Scope != "main" || seen.MatchText != "Copy" || seen.Limit != 10 || seen.Offset != 5 {
		t.Fatalf("plumbed action mismatch: %+v", seen)
	}
	if !strings.Contains(out, `"total": 42`) || !strings.Contains(out, `"truncated": true`) {
		t.Fatalf("Total/Truncated not serialized; got %s", out)
	}
}

// TestBrowserScreenshotFieldsPlumbed checks the new max_bytes /
// save_path are forwarded so the Go-side postprocess can act on them.
func TestBrowserScreenshotFieldsPlumbed(t *testing.T) {
	var seen wire.Action
	client := &fakeClient{
		exec: func(_ context.Context, a wire.Action) (*wire.Result, error) {
			seen = a
			return &wire.Result{Success: true, Path: "/tmp/x.png"}, nil
		},
	}
	ctx := browser.WithClient(context.Background(), client)
	reg := NewRegistry()
	registerBrowserScreenshot(reg)
	spec, _ := reg.Get("browser_screenshot")
	out, err := spec.Handler(ctx, json.RawMessage(`{"max_bytes":200000,"save_path":"my.png"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if seen.MaxBytes != 200000 || seen.SavePath != "my.png" {
		t.Fatalf("plumbed: %+v", seen)
	}
	if !strings.Contains(out, "/tmp/x.png") {
		t.Fatalf("path not in result: %s", out)
	}
}

// TestBrowserNewActionsRegistered confirms every Phase 3–5 tool is in
// the registry. Guards against a missing line in RegisterBrowserHandlers.
func TestBrowserNewActionsRegistered(t *testing.T) {
	reg := NewRegistry()
	RegisterBrowserHandlers(reg)
	expected := []string{
		"browser_wait_for_selector",
		"browser_keyboard_press",
		"browser_clipboard_read",
		"browser_clipboard_write",
		"browser_cookies_get",
		"browser_storage_get",
		"browser_capabilities",
	}
	for _, name := range expected {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

// TestBrowserActionsRouteCorrectly walks every new action and asserts
// the wire.Action.Type sent to the client matches the registered tool.
func TestBrowserActionsRouteCorrectly(t *testing.T) {
	cases := []struct {
		tool   string
		action string
		input  string
	}{
		{"browser_wait_for_selector", wire.ActionWaitForSelector, `{"selector":"#x"}`},
		{"browser_keyboard_press", wire.ActionKeyboardPress, `{"key":"Enter"}`},
		{"browser_clipboard_read", wire.ActionClipboardRead, `{}`},
		{"browser_clipboard_write", wire.ActionClipboardWrite, `{"text":"hi"}`},
		{"browser_cookies_get", wire.ActionCookiesGet, `{"name":"sid"}`},
		{"browser_storage_get", wire.ActionStorageGet, `{"storage":"local"}`},
		{"browser_capabilities", wire.ActionCapabilities, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			var got wire.Action
			client := &fakeClient{
				exec: func(_ context.Context, a wire.Action) (*wire.Result, error) {
					got = a
					return &wire.Result{Success: true}, nil
				},
			}
			ctx := browser.WithClient(context.Background(), client)
			reg := NewRegistry()
			RegisterBrowserHandlers(reg)
			spec, ok := reg.Get(tc.tool)
			if !ok {
				t.Fatalf("tool not registered")
			}
			if _, err := spec.Handler(ctx, json.RawMessage(tc.input)); err != nil {
				t.Fatalf("handler: %v", err)
			}
			if got.Type != tc.action {
				t.Fatalf("action.Type = %q, want %q", got.Type, tc.action)
			}
		})
	}
}

package live

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// TestActionToParams_Evaluate pins the routing for the evaluate
// action: live mode now supports it, so the dispatcher must produce a
// {"evaluate", {"script": "..."}} pair — not the legacy
// "action not supported" error that drove agents to write
// ad-hoc Python decode dances when they wanted to inspect page state.
func TestActionToParams_Evaluate(t *testing.T) {
	method, params, err := actionToParams(mcpservers.BrowserAction{
		Type:   mcpservers.ActionEvaluate,
		Script: "document.title",
	})
	if err != nil {
		t.Fatalf("actionToParams(evaluate): unexpected err: %v", err)
	}
	if method != "evaluate" {
		t.Fatalf("method = %q; want evaluate", method)
	}
	if got, _ := params["script"].(string); got != "document.title" {
		t.Fatalf("params.script = %v; want document.title", params["script"])
	}
}

func TestActionToParams_UnknownStillErrors(t *testing.T) {
	if _, _, err := actionToParams(mcpservers.BrowserAction{Type: "does-not-exist"}); err == nil {
		t.Fatal("expected error for unknown action; got nil")
	}
}

// TestActionToParamsCoversAllActions enumerates every wire.Action
// type the live service is expected to dispatch, and asserts each
// produces the right method name (i.e. is actually wired).
func TestActionToParamsCoversAllActions(t *testing.T) {
	cases := []struct {
		name   string
		action mcpservers.BrowserAction
		method string
	}{
		{"navigate", mcpservers.BrowserAction{Type: mcpservers.ActionNavigate, URL: "https://x"}, "navigate"},
		{"click-selector", mcpservers.BrowserAction{Type: mcpservers.ActionClick, Selector: ".x"}, "click"},
		{"click-text", mcpservers.BrowserAction{Type: mcpservers.ActionClick, MatchText: "Copy"}, "click"},
		{"type", mcpservers.BrowserAction{Type: mcpservers.ActionType, Selector: "#q", Text: "hi"}, "type"},
		{"scroll", mcpservers.BrowserAction{Type: mcpservers.ActionScroll, Direction: "down"}, "scroll"},
		{"screenshot", mcpservers.BrowserAction{Type: mcpservers.ActionScreenshot}, "screenshot"},
		{"extract", mcpservers.BrowserAction{Type: mcpservers.ActionExtract, Scope: "main", Limit: 5}, "extract"},
		{"back", mcpservers.BrowserAction{Type: mcpservers.ActionBack}, "back"},
		{"tabs", mcpservers.BrowserAction{Type: mcpservers.ActionTabs, TabAction: "list"}, "tabs"},
		{"evaluate", mcpservers.BrowserAction{Type: mcpservers.ActionEvaluate, Script: "1+1"}, "evaluate"},
		{"wait-for-selector", mcpservers.BrowserAction{Type: mcpservers.ActionWaitForSelector, Selector: ".x"}, "wait_for_selector"},
		{"keyboard-press", mcpservers.BrowserAction{Type: mcpservers.ActionKeyboardPress, Key: "Enter"}, "keyboard_press"},
		{"clipboard-read", mcpservers.BrowserAction{Type: mcpservers.ActionClipboardRead}, "clipboard_read"},
		{"clipboard-write", mcpservers.BrowserAction{Type: mcpservers.ActionClipboardWrite, Text: "x"}, "clipboard_write"},
		{"cookies-get", mcpservers.BrowserAction{Type: mcpservers.ActionCookiesGet, Name: "sid"}, "cookies_get"},
		{"storage-get", mcpservers.BrowserAction{Type: mcpservers.ActionStorageGet, Storage: "local"}, "storage_get"},
		{"capabilities", mcpservers.BrowserAction{Type: mcpservers.ActionCapabilities}, "capabilities"},
		{"network-list", mcpservers.BrowserAction{Type: mcpservers.ActionNetworkList}, "network_list"},
		{"console-get", mcpservers.BrowserAction{Type: mcpservers.ActionConsoleGet}, "console_get"},
		{"perf-start", mcpservers.BrowserAction{Type: mcpservers.ActionPerfStart}, "perf_start"},
		{"perf-stop", mcpservers.BrowserAction{Type: mcpservers.ActionPerfStop}, "perf_stop"},
		{"lighthouse", mcpservers.BrowserAction{Type: mcpservers.ActionLighthouse}, "lighthouse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method, params, err := actionToParams(tc.action)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if method != tc.method {
				t.Fatalf("method = %q, want %q", method, tc.method)
			}
			if params == nil {
				t.Fatalf("nil params")
			}
		})
	}
}

// TestActionToParamsClickFields ensures the new click fields are
// passed to the extension verbatim. Regression for the
// retrospective's "Copy / show" buttons that needed match_text + scope.
func TestActionToParamsClickFields(t *testing.T) {
	method, params, err := actionToParams(mcpservers.BrowserAction{
		Type:      mcpservers.ActionClick,
		Selector:  "button.copy",
		MatchText: "Copy",
		Scope:     "main",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if method != "click" {
		t.Fatalf("method")
	}
	want := map[string]any{
		"selector":   "button.copy",
		"element_id": 0,
		"match_text": "Copy",
		"scope":      "main",
	}
	if !reflect.DeepEqual(params, want) {
		t.Fatalf("params = %+v, want %+v", params, want)
	}
}

// TestVersionLess covers the dotted-numeric comparator used to detect
// extension drift.
func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.2.0", "0.3.0", true},
		{"0.3.0", "0.3.0", false},
		{"0.3.1", "0.3.0", false},
		{"1.0.0", "0.99.0", false},
		{"", "0.0.1", true},
		{"0.0.1", "", false},
	}
	for _, tc := range cases {
		if got := versionLess(tc.a, tc.b); got != tc.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestNotConnectedErrorIncludesLastTab — the new error string surfaces
// the last-known tab URL so a reattach lands on the right tab.
func TestNotConnectedErrorIncludesLastTab(t *testing.T) {
	msg := notConnectedError("https://cloud.digitalocean.com/databases/abc")
	if !strings.Contains(msg, "https://cloud.digitalocean.com") {
		t.Fatalf("last tab URL missing: %q", msg)
	}
	if !strings.Contains(msg, "ycode browser setup live") {
		t.Fatalf("setup hint missing: %q", msg)
	}
	if !strings.Contains(msg, "chrome://extensions") {
		t.Fatalf("extensions URL hint missing: %q", msg)
	}
}

// TestNotConnectedErrorWithoutLastTab — no URL clipped means the
// message is still useful (no orphan "last tab: " suffix).
func TestNotConnectedErrorWithoutLastTab(t *testing.T) {
	msg := notConnectedError("")
	if strings.Contains(msg, "last tab:") {
		t.Fatalf("orphan 'last tab:' present when no tab known: %q", msg)
	}
}

// stubServiceWithHub returns a Service in roleHub with a hand-rolled
// hub that has the given hello state. Used to drive the staleness /
// per-method gates without needing a real WS round-trip.
func stubServiceWithHub(version string, methods []string) *Service {
	s := New(0)
	s.role = roleHub
	s.hub = &hub{
		extVersion: version,
		extMethods: methods,
	}
	return s
}

// TestLive_StaleExtensionError — Execute must short-circuit with the
// actionable "extension stale" error before attempting any dispatch
// when the extension's _hello reports a version below the floor. This
// is the cure for the prod-e2e incident where an old extension returned
// cryptic "unknown method: X" on every call.
func TestLive_StaleExtensionError(t *testing.T) {
	s := stubServiceWithHub("0.4.0", nil) // below 0.5.0 floor
	res, err := s.Execute(context.Background(), mcpservers.BrowserAction{
		Type:   mcpservers.ActionEvaluate,
		Script: "1+1",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Error == "" {
		t.Fatalf("expected non-empty error from stale gate")
	}
	if !strings.Contains(res.Error, "extension stale") {
		t.Fatalf("expected 'extension stale' in error, got: %q", res.Error)
	}
	if !strings.Contains(res.Error, "0.4.0") || !strings.Contains(res.Error, "0.5.0") {
		t.Fatalf("expected both versions in error, got: %q", res.Error)
	}
	if !strings.Contains(res.Error, "chrome://extensions") {
		t.Fatalf("expected chrome://extensions remediation hint, got: %q", res.Error)
	}
}

// TestLive_PerMethodGate — when the extension is at the right version
// but its _hello methods list omits the requested method, Execute must
// short-circuit with a method-not-advertised error. This catches users
// whose extension is technically at the floor but is missing a method
// the binary now expects.
func TestLive_PerMethodGate(t *testing.T) {
	s := stubServiceWithHub("0.5.0", []string{"navigate", "extract"})
	res, err := s.Execute(context.Background(), mcpservers.BrowserAction{
		Type:   mcpservers.ActionEvaluate,
		Script: "1+1",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Error == "" {
		t.Fatalf("expected non-empty error from per-method gate")
	}
	if !strings.Contains(res.Error, "evaluate") {
		t.Fatalf("expected method name in error, got: %q", res.Error)
	}
	if !strings.Contains(res.Error, "not advertised") {
		t.Fatalf("expected 'not advertised' phrasing, got: %q", res.Error)
	}
}

// TestLive_CapabilitiesBypassesStaleGate — `capabilities` is the one
// action that must reach a stale extension so `ycode browser doctor`
// can introspect it. The gate must not short-circuit it, even when the
// reported version is below the floor.
func TestLive_CapabilitiesBypassesStaleGate(t *testing.T) {
	s := stubServiceWithHub("0.1.0", nil)
	res, _ := s.Execute(context.Background(), mcpservers.BrowserAction{
		Type: mcpservers.ActionCapabilities,
	})
	// Either the call reaches the (nil) socket and errors at the WS
	// layer, or it returns some default. The key invariant: the gate
	// must NOT have short-circuited it with the stale-extension error.
	if strings.Contains(res.Error, "extension stale") {
		t.Fatalf("capabilities should bypass the stale gate, but got: %q", res.Error)
	}
}

// TestLive_PerMethodGate_EmptyMethodsListSkipped — pre-0.4 extensions
// don't send methods in their _hello (legacy). The per-method gate
// must skip the check rather than block every call. (The version gate
// will catch the staleness first anyway, but defence in depth.)
func TestLive_PerMethodGate_EmptyMethodsListSkipped(t *testing.T) {
	// At the floor version with no methods list — gate should NOT block.
	s := stubServiceWithHub("0.5.0", nil)
	res, _ := s.Execute(context.Background(), mcpservers.BrowserAction{
		Type:   mcpservers.ActionEvaluate,
		Script: "1+1",
	})
	if strings.Contains(res.Error, "not advertised") {
		t.Fatalf("empty methods list should skip per-method gate, got: %q", res.Error)
	}
}

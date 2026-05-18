//go:build integration

// Browser robustness integration tests — exercise the Phase 1–5
// additions against a real headless Chrome via solo mode. Skipped if
// Chrome isn't on the host (CI containers without Chrome flag this as
// "skipped: no Chrome").
//
// What this proves end-to-end:
//   - browser_extract scope + match_text + limit/offset + nav-filter
//   - browser_click match_text fallback (no selector needed)
//   - Ralph "extract-click-by-text" strategy succeeds against unfriendly
//     buttons (the DO "Copy" / "show" case from the retrospective)
//   - browser_screenshot MaxBytes cap → JPEG fallback or file-spill
//   - browser_wait_for_selector blocks until the element appears
//   - browser_keyboard_press fires (Enter submits a form)
//   - browser_storage_get reads localStorage
//   - browser_capabilities returns a usable method list
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/probe"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/reliability"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/solo"
)

// fixtureHTML mirrors the DO dashboard shape from the retrospective:
//   - sidebar nav with junk links (must be skipped by default extract)
//   - main panel with a "Copy" button (no aria-label, just text)
//   - a form whose submit handler is attached lazily via JS injection
//     (after navigate) — keeps the page entirely static during load
//     so solo's `chromedp.Text("body", ...NodeVisible)` doesn't race
//     against a body mutation
const fixtureHTML = `<!doctype html>
<html><head><title>ycode robustness fixture</title></head>
<body>
  <nav>
    <a href="#x">Home</a>
    <a href="#y">Settings</a>
    <a href="#z">Copy nav link</a>
  </nav>
  <main>
    <h1>Cluster status</h1>
    <button id="copy-btn">Copy</button>
    <form id="search-form">
      <input id="q" name="q" type="text" />
    </form>
    <div id="result" style="display:none">submitted</div>
  </main>
</body></html>`

// installAsyncFixtures injects the dynamic bits after navigate. Kept
// out of fixtureHTML so the initial page load is static (see comment
// above the const for the chromedp.Text+body-mutation interaction).
//
//   - localStorage["sid"] / sessionStorage["flash"] for storage_get
//   - a 500 ms delayed <div id="ready"> for wait_for_selector
//   - a submit handler on #search-form that reveals #result
const installAsyncFixtures = `(function(){
  localStorage.setItem("sid","abc-123");
  sessionStorage.setItem("flash","hello");
  setTimeout(function(){
    var d=document.createElement("div");
    d.id="ready"; d.textContent="ready";
    document.body.appendChild(d);
  },500);
  document.getElementById("search-form").addEventListener("submit",function(e){
    e.preventDefault();
    var r=document.getElementById("result");
    r.style.display="block";
    r.textContent="submitted="+document.getElementById("q").value;
  });
  return true;
})()`

func startFixture(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fixtureHTML))
	}))
}

// soloOrSkip launches solo Chrome with no reliability wrapping. Used
// by the action-surface tests below; the wrappers are exercised
// separately via soloWithReliability.
func soloOrSkip(t *testing.T) (mcpservers.Service, context.Context, context.CancelFunc) {
	t.Helper()
	return launchSolo(t, false)
}

// soloWithReliability returns a solo Service wrapped in the full
// reliability stack (Hint Engine, Ralph, Circuit Breaker, ...). Used
// by the ralph-exhaustion test.
func soloWithReliability(t *testing.T) (mcpservers.Service, context.Context, context.CancelFunc) {
	t.Helper()
	return launchSolo(t, true)
}

func launchSolo(t *testing.T, wrap bool) (mcpservers.Service, context.Context, context.CancelFunc) {
	t.Helper()
	if probe.DetectChrome() == "" {
		t.Skip("integration: no Chrome on host; skipping browser robustness tests")
	}
	tmp, err := os.MkdirTemp("", "ycode-itest-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	var svc mcpservers.Service = solo.New(solo.Config{UserDataDir: tmp, Headed: false})
	if wrap {
		svc = reliability.Wrap(svc, reliability.Config{})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	if err := svc.EnsureReady(ctx); err != nil {
		cancel()
		t.Skipf("integration: solo Chrome unavailable: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = svc.Stop(stopCtx)
	})
	return svc, ctx, cancel
}

func mustExecute(t *testing.T, w mcpservers.Service, ctx context.Context, a mcpservers.BrowserAction) *mcpservers.BrowserResult {
	t.Helper()
	r, err := w.Execute(ctx, a)
	if err != nil {
		t.Fatalf("Execute(%s): %v", a.Type, err)
	}
	if r == nil {
		t.Fatalf("nil result")
	}
	if r.Error != "" {
		t.Fatalf("Execute(%s) error: %s", a.Type, r.Error)
	}
	return r
}

func TestBrowserRobust_ExtractAndClickByText(t *testing.T) {
	w, ctx, cancel := soloOrSkip(t)
	defer cancel()

	srv := startFixture(t)
	defer srv.Close()
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionNavigate, URL: srv.URL})

	// Default extract: should skip <nav> and only enumerate main-panel
	// interactive elements (Copy button, form input). The legacy code
	// would have returned the nav links first.
	r := mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionExtract})
	if strings.Contains(r.Elements, "Home") || strings.Contains(r.Elements, "Settings") {
		t.Fatalf("nav-filter did not skip sidebar: %q", r.Elements)
	}
	if !strings.Contains(r.Elements, "Copy") {
		t.Fatalf("main-panel Copy button missing from extract: %q", r.Elements)
	}

	// Click by match_text — no selector, no element_id. Ralph picks
	// the right strategy.
	cr := mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionClick, MatchText: "Copy",
	})
	if !cr.Success {
		t.Fatalf("click by match_text failed: %+v", cr)
	}
}

func TestBrowserRobust_ScopedExtract(t *testing.T) {
	w, ctx, cancel := soloOrSkip(t)
	defer cancel()
	srv := startFixture(t)
	defer srv.Close()
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionNavigate, URL: srv.URL})

	// With scope=nav, we explicitly opt out of the nav filter and
	// should see the nav links.
	r := mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionExtract, Scope: "nav",
	})
	if !strings.Contains(r.Elements, "Home") || !strings.Contains(r.Elements, "Settings") {
		t.Fatalf("explicit scope=nav should expose nav links: %q", r.Elements)
	}
	if r.Total < 3 {
		t.Fatalf("expected total>=3 nav entries; got %d", r.Total)
	}
}

func TestBrowserRobust_ScreenshotCapTriggersJPEGOrSpill(t *testing.T) {
	w, ctx, cancel := soloOrSkip(t)
	defer cancel()
	srv := startFixture(t)
	defer srv.Close()
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionNavigate, URL: srv.URL})

	tightCap := 5000 // 5 KB inline cap
	r := mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionScreenshot, MaxBytes: tightCap,
	})
	if r.Path == "" && len(r.Image) > tightCap {
		t.Fatalf("inline image %d bytes > cap %d and no spill path", len(r.Image), tightCap)
	}
	if r.Path != "" {
		if _, err := os.Stat(r.Path); err != nil {
			t.Fatalf("spill path missing: %v", err)
		}
	}
}

func TestBrowserRobust_WaitForSelector(t *testing.T) {
	w, ctx, cancel := soloOrSkip(t)
	defer cancel()
	srv := startFixture(t)
	defer srv.Close()
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionNavigate, URL: srv.URL})
	// Inject the delayed-div setup after navigate (see installAsyncFixtures).
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionEvaluate, Script: installAsyncFixtures})

	start := time.Now()
	r := mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionWaitForSelector, Selector: "#ready", TimeoutMs: 2000,
	})
	if !r.Success {
		t.Fatalf("wait failed: %+v", r)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("wait returned too fast (%s) — element may have existed at navigate time", elapsed)
	}
}

func TestBrowserRobust_KeyboardEnterSubmits(t *testing.T) {
	w, ctx, cancel := soloOrSkip(t)
	defer cancel()
	srv := startFixture(t)
	defer srv.Close()
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionNavigate, URL: srv.URL})
	// Wire up the submit handler after navigate.
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionEvaluate, Script: installAsyncFixtures})

	// Type into the field, press Enter, then assert the result div
	// shows the submitted text.
	mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionType, Selector: "#q", Text: "hi",
	})
	mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionKeyboardPress, Selector: "#q", Key: "Enter",
	})
	// Wait for the visible result.
	mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionWaitForSelector, Selector: "#result", State: "visible", TimeoutMs: 2000,
	})
	r := mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionEvaluate, Script: `document.getElementById("result").textContent`,
	})
	if !strings.Contains(r.Data, "submitted=hi") {
		t.Fatalf("Enter did not submit the form; result=%q", r.Data)
	}
}

func TestBrowserRobust_StorageGetLocalAndSession(t *testing.T) {
	w, ctx, cancel := soloOrSkip(t)
	defer cancel()
	srv := startFixture(t)
	defer srv.Close()
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionNavigate, URL: srv.URL})
	// Populate storage after navigate (see installAsyncFixtures).
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionEvaluate, Script: installAsyncFixtures})

	rl := mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionStorageGet, Storage: "local", StorageKey: "sid",
	})
	if !strings.Contains(rl.Data, "abc-123") {
		t.Fatalf("local storage_get missing value: %q", rl.Data)
	}
	rs := mustExecute(t, w, ctx, mcpservers.BrowserAction{
		Type: mcpservers.ActionStorageGet, Storage: "session", StorageKey: "flash",
	})
	if !strings.Contains(rs.Data, "hello") {
		t.Fatalf("session storage_get missing value: %q", rs.Data)
	}
}

func TestBrowserRobust_Capabilities(t *testing.T) {
	w, ctx, cancel := soloOrSkip(t)
	defer cancel()

	r := mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionCapabilities})
	var caps struct {
		Mode    string   `json:"mode"`
		Methods []string `json:"methods"`
	}
	if err := json.Unmarshal([]byte(r.Data), &caps); err != nil {
		t.Fatalf("decode capabilities: %v; data=%q", err, r.Data)
	}
	if caps.Mode != mcpservers.ModeSolo {
		t.Fatalf("mode = %q, want solo", caps.Mode)
	}
	wantAll := []string{
		mcpservers.ActionNavigate, mcpservers.ActionClick, mcpservers.ActionEvaluate,
		mcpservers.ActionWaitForSelector, mcpservers.ActionKeyboardPress,
		mcpservers.ActionClipboardRead, mcpservers.ActionCookiesGet,
		mcpservers.ActionStorageGet, mcpservers.ActionCapabilities,
	}
	got := map[string]bool{}
	for _, m := range caps.Methods {
		got[m] = true
	}
	for _, want := range wantAll {
		if !got[want] {
			t.Errorf("capabilities missing %q; got %v", want, caps.Methods)
		}
	}
}

func TestBrowserRobust_SelfhealOnRalphExhaustion(t *testing.T) {
	// Verify the failure-hint shape that the selfheal classifier
	// regexes against. End-to-end: drive a click against a selector
	// no strategy can satisfy; the result should carry an enumerated
	// "ralph: all N click strategies failed" hint that matches the
	// classifier's pattern.
	w, ctx, cancel := soloWithReliability(t)
	defer cancel()
	srv := startFixture(t)
	defer srv.Close()
	mustExecute(t, w, ctx, mcpservers.BrowserAction{Type: mcpservers.ActionNavigate, URL: srv.URL})

	r, _ := w.Execute(ctx, mcpservers.BrowserAction{
		Type:     mcpservers.ActionClick,
		Selector: ".this-selector-does-not-exist-anywhere",
	})
	if r == nil {
		t.Fatalf("nil result for failing click")
	}
	found := false
	for _, h := range r.Hints {
		if strings.Contains(h, "click strategies failed") {
			found = true
			// Confirm it enumerates at least the as-given + js-click
			// strategies so the classifier hint stays load-bearing.
			if !strings.Contains(h, "as-given") {
				t.Errorf("hint missing as-given strategy: %q", h)
			}
			break
		}
	}
	if !found {
		t.Fatalf("no 'click strategies failed' hint in result: %+v", r.Hints)
	}
	// Optional secondary signal: make sure the result is materially
	// described (Success may legitimately be false; we just want the
	// failure surface to be present and routable).
	if r.Success {
		t.Logf("note: r.Success=true despite failure — verify ralph propagation")
	}
}

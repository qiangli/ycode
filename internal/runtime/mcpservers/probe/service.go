// Package probe is ycode's "probe" browser mode — pure-Go CDP attach
// to a Chrome started with --remote-debugging-port. Drives real
// DevTools data (perf traces, network waterfalls, source-mapped
// console). Built on chromedp.
package probe

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// DefaultURL is the conventional Chrome debug endpoint.
const DefaultURL = "http://localhost:9222"

// Service is the probe-mode backend.
type Service struct {
	url string

	mu        sync.Mutex
	allocCtx  context.Context
	allocStop context.CancelFunc
	ctx       context.Context
	ctxStop   context.CancelFunc

	// dev owns DevTools-flavored long-lived state (network + console
	// ring buffers, trace recording state). Populated by
	// installListeners on EnsureReady.
	dev devtools
}

// New returns a probe-mode service. url defaults to
// http://localhost:9222 when empty.
func New(url string) *Service {
	if url == "" {
		url = DefaultURL
	}
	return &Service{url: url}
}

func (s *Service) Name() string { return mcpservers.ModeProbe }
func (s *Service) URL() string  { return s.url }

// Available probes the debug endpoint with a short HTTP GET on the
// /json/version metadata path. Cheap and non-destructive.
func (s *Service) Available(ctx context.Context) bool {
	hctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, s.url+"/json/version", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// EnsureReady opens the chromedp allocator + context.
func (s *Service) EnsureReady(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx != nil {
		return nil
	}
	if !s.Available(ctx) {
		return fmt.Errorf("probe: no Chrome at %s — start it with `ycode browser launch`", s.url)
	}

	allocCtx, allocStop := chromedp.NewRemoteAllocator(context.Background(), s.url)
	cdpCtx, cdpStop := chromedp.NewContext(allocCtx)

	// Force initial protocol handshake; chromedp lazily attaches on
	// the first Run, but doing it here surfaces errors early.
	if err := chromedp.Run(cdpCtx); err != nil {
		cdpStop()
		allocStop()
		return fmt.Errorf("probe: attach to %s: %w", s.url, err)
	}

	s.allocCtx, s.allocStop = allocCtx, allocStop
	s.ctx, s.ctxStop = cdpCtx, cdpStop

	// Hook the long-lived event listener for the DevTools-flavored
	// actions (network_list, console_get, perf_*). Failure here is
	// non-fatal — the basic actions (navigate/click/type) work
	// without it; only the DevTools surface degrades.
	if err := s.dev.installListeners(cdpCtx); err != nil {
		slog.Warn("probe: install DevTools listeners failed", "error", err)
	}
	slog.Info("probe: attached", "url", s.url)
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctxStop != nil {
		s.ctxStop()
		s.ctxStop = nil
	}
	if s.allocStop != nil {
		s.allocStop()
		s.allocStop = nil
	}
	s.ctx = nil
	s.allocCtx = nil
	return nil
}

// Execute dispatches a BrowserAction to chromedp. Each call gets a
// derived context with a per-call timeout so a hung page doesn't
// wedge the whole service.
func (s *Service) Execute(ctx context.Context, action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	s.mu.Lock()
	cdpCtx := s.ctx
	s.mu.Unlock()
	if cdpCtx == nil {
		return nil, errors.New("probe: not ready (call EnsureReady first)")
	}

	callCtx, cancel := context.WithTimeout(cdpCtx, 30*time.Second)
	defer cancel()

	switch action.Type {
	case mcpservers.ActionNavigate:
		return s.doNavigate(callCtx, action.URL)
	case mcpservers.ActionClick:
		return s.doClick(callCtx, action)
	case mcpservers.ActionType:
		return s.doType(callCtx, action)
	case mcpservers.ActionScroll:
		return s.doScroll(callCtx, action)
	case mcpservers.ActionScreenshot:
		return s.doScreenshot(callCtx, action)
	case mcpservers.ActionExtract:
		return s.doExtract(callCtx, action)
	case mcpservers.ActionBack:
		return s.doBack(callCtx)
	case mcpservers.ActionTabs:
		return s.doTabs(callCtx, action)
	case mcpservers.ActionEvaluate:
		return s.doEvaluate(callCtx, action.Script)
	case mcpservers.ActionPerfStart:
		return s.doPerfStart(callCtx)
	case mcpservers.ActionPerfStop:
		return s.doPerfStop(callCtx)
	case mcpservers.ActionNetworkList:
		return s.doNetworkList()
	case mcpservers.ActionConsoleGet:
		return s.doConsoleGet()
	case mcpservers.ActionLighthouse:
		return s.doLighthouse(callCtx)
	case mcpservers.ActionWaitForSelector:
		return s.doWaitForSelector(callCtx, action)
	case mcpservers.ActionKeyboardPress:
		return s.doKeyboardPress(callCtx, action)
	case mcpservers.ActionClipboardRead:
		return s.doClipboardRead(callCtx)
	case mcpservers.ActionClipboardWrite:
		return s.doClipboardWrite(callCtx, action)
	case mcpservers.ActionCookiesGet:
		return s.doCookiesGet(callCtx, action)
	case mcpservers.ActionStorageGet:
		return s.doStorageGet(callCtx, action)
	case mcpservers.ActionCapabilities:
		return s.doCapabilities()
	}
	return &mcpservers.BrowserResult{
		Error: fmt.Sprintf("probe: action %q not supported", action.Type),
	}, nil
}

// --- action implementations ---

func (s *Service) doNavigate(ctx context.Context, url string) (*mcpservers.BrowserResult, error) {
	if url == "" {
		return &mcpservers.BrowserResult{Error: "navigate: url required"}, nil
	}
	var title, currentURL, body string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Title(&title),
		chromedp.Location(&currentURL),
		chromedp.Text("body", &body, chromedp.NodeVisible),
	)
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{
		Success: true,
		Title:   title,
		URL:     currentURL,
		Content: truncate(body, 16000),
	}, nil
}

func (s *Service) doClick(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	if a.Selector != "" {
		if err := chromedp.Run(ctx, chromedp.Click(a.Selector, chromedp.ByQuery)); err != nil {
			return &mcpservers.BrowserResult{Error: err.Error()}, nil
		}
		return &mcpservers.BrowserResult{Success: true}, nil
	}
	if a.MatchText != "" {
		js := `(function(){
  var want = ` + jsString(a.MatchText) + `.toLowerCase();
  var scope = ` + jsString(a.Scope) + `;
  var root = scope ? document.querySelector(scope) : document;
  if (!root) return false;
  var nodes = root.querySelectorAll("a, button, input[type=button], input[type=submit], [role='button'], [role='link']");
  for (var i=0; i<nodes.length; i++) {
    var n = nodes[i];
    var v = ((n.innerText) || n.value || n.getAttribute("aria-label") || "").trim().toLowerCase();
    if (v.indexOf(want) >= 0) { n.click(); return true; }
  }
  return false;
})()`
		var out any
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &out)); err != nil {
			return &mcpservers.BrowserResult{Error: err.Error()}, nil
		}
		if b, ok := out.(bool); ok && b {
			return &mcpservers.BrowserResult{Success: true}, nil
		}
		return &mcpservers.BrowserResult{Error: "click: no element matched match_text"}, nil
	}
	return &mcpservers.BrowserResult{Error: "click: selector or match_text required"}, nil
}

func (s *Service) doType(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	sel := a.Selector
	if sel == "" {
		return &mcpservers.BrowserResult{Error: "type: selector required"}, nil
	}
	if err := chromedp.Run(ctx, chromedp.SendKeys(sel, a.Text, chromedp.ByQuery)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true}, nil
}

func (s *Service) doScroll(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	amount := a.Amount
	if amount == 0 {
		amount = 500
	}
	if a.Direction == "up" {
		amount = -amount
	}
	script := fmt.Sprintf("window.scrollBy(0, %d); window.scrollY", amount)
	var y float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &y)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: fmt.Sprintf("scrollY=%g", y)}, nil
}

func (s *Service) doScreenshot(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	if a.MaxBytes > 0 || a.SavePath != "" {
		img, path, err := mcpservers.PostprocessScreenshot(buf, a)
		if err != nil {
			return &mcpservers.BrowserResult{Error: err.Error()}, nil
		}
		return &mcpservers.BrowserResult{Success: true, Image: img, Path: path}, nil
	}
	return &mcpservers.BrowserResult{
		Success: true,
		Image:   base64.StdEncoding.EncodeToString(buf),
	}, nil
}

func (s *Service) doExtract(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	var title, url string
	if err := chromedp.Run(ctx, chromedp.Title(&title), chromedp.Location(&url)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	// Run a JS snippet that does scope-aware + nav-filtered element
	// enumeration so probe/solo and live share semantics. Matches the
	// extension's runInTab extract path.
	js := extractScript(a)
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	out := parseExtractPayload(raw)
	out.Title = title
	out.URL = url
	return out, nil
}

// extractScript builds the JS one-liner the extract action runs to
// enumerate interactive elements. The same source is used by live's
// background.js (extractInTab) — kept here as a string so probe/solo
// can run it via chromedp.Evaluate and stay byte-compatible. Returns
// a JSON string the Go side decodes via parseExtractPayload.
func extractScript(a mcpservers.BrowserAction) string {
	scope := jsString(a.Scope)
	match := jsString(a.MatchText)
	if match == "" {
		match = jsString(a.Goal)
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := a.Offset
	if offset < 0 {
		offset = 0
	}
	return `(function(){
  var SCOPE_SEL=` + scope + `, MATCH=` + match + `, LIMIT=` + fmt.Sprintf("%d", limit) + `, OFFSET=` + fmt.Sprintf("%d", offset) + `;
  var root = SCOPE_SEL ? document.querySelector(SCOPE_SEL) : document;
  if (!root) return JSON.stringify({error:"extract: scope "+SCOPE_SEL+" not found"});
  var navFilter = !SCOPE_SEL;
  var all = root.querySelectorAll("a, button, input, select, textarea, [role='button'], [role='link']");
  var matches = [];
  for (var i=0; i<all.length; i++) {
    var el = all[i];
    if (navFilter && el.closest && el.closest("nav, aside, [role='navigation'], [role='complementary']")) continue;
    var text = (el.innerText || el.value || el.getAttribute("aria-label") || "").trim();
    if (MATCH && text.toLowerCase().indexOf(MATCH.toLowerCase()) < 0) {
      var ph = el.getAttribute && el.getAttribute("placeholder");
      var ar = el.getAttribute && el.getAttribute("aria-label");
      if (!(ph && ph.toLowerCase().indexOf(MATCH.toLowerCase())>=0) && !(ar && ar.toLowerCase().indexOf(MATCH.toLowerCase())>=0)) continue;
    }
    matches.push(el);
  }
  var total = matches.length;
  var slice = matches.slice(OFFSET, OFFSET+LIMIT);
  var lines = [];
  for (var j=0; j<slice.length; j++) {
    var el = slice[j];
    var tag = el.tagName.toLowerCase();
    var text = (el.innerText || el.value || el.getAttribute("aria-label") || "").trim().slice(0,80);
    var attrs = [];
    var keys = ["type","placeholder","href","name","value","role","aria-label"];
    for (var k=0; k<keys.length; k++) {
      var v = el.getAttribute(keys[k]);
      if (v) attrs.push(keys[k]+"=\""+String(v).slice(0,60)+"\"");
    }
    lines.push("["+(OFFSET+j+1)+"] <"+tag+" "+attrs.join(" ")+">"+text+"</"+tag+">");
  }
  var body = (root === document ? (document.body && document.body.innerText) : root.innerText) || "";
  return JSON.stringify({
    content: body.length>16000 ? body.slice(0,16000)+"\n... (truncated)" : body,
    elements: lines.join("\n"),
    total: total,
    truncated: total > (OFFSET+LIMIT),
  });
})()`
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func parseExtractPayload(raw string) *mcpservers.BrowserResult {
	var inner struct {
		Content   string `json:"content"`
		Elements  string `json:"elements"`
		Total     int    `json:"total"`
		Truncated bool   `json:"truncated"`
		Error     string `json:"error"`
	}
	_ = json.Unmarshal([]byte(raw), &inner)
	if inner.Error != "" {
		return &mcpservers.BrowserResult{Error: inner.Error}
	}
	return &mcpservers.BrowserResult{
		Success:   true,
		Content:   inner.Content,
		Elements:  inner.Elements,
		Total:     inner.Total,
		Truncated: inner.Truncated,
	}
}

func (s *Service) doBack(ctx context.Context) (*mcpservers.BrowserResult, error) {
	if err := chromedp.Run(ctx, chromedp.NavigateBack()); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true}, nil
}

func (s *Service) doTabs(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	// Phase 2 ships list only. Full multi-tab orchestration lands
	// with solo mode in Phase 3 + reliability layer.
	switch a.TabAction {
	case "list", "":
		targets, err := chromedp.Targets(ctx)
		if err != nil {
			return &mcpservers.BrowserResult{Error: err.Error()}, nil
		}
		var b []byte
		for i, t := range targets {
			b = append(b, []byte(fmt.Sprintf("[%d] %s\n    %s\n", i+1, t.Title, t.URL))...)
		}
		return &mcpservers.BrowserResult{Success: true, Content: string(b)}, nil
	}
	return &mcpservers.BrowserResult{
		Error: fmt.Sprintf("probe: tab action %q not yet supported", a.TabAction),
	}, nil
}

func (s *Service) doEvaluate(ctx context.Context, script string) (*mcpservers.BrowserResult, error) {
	if script == "" {
		return &mcpservers.BrowserResult{Error: "evaluate: script required"}, nil
	}
	var out any
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &out)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: fmt.Sprintf("%v", out)}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n... (truncated)"
}

// --- Phase 3–5 actions ---

func (s *Service) doWaitForSelector(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	sel := a.Selector
	if sel == "" {
		return &mcpservers.BrowserResult{Error: "wait_for_selector: selector required"}, nil
	}
	timeout := time.Duration(a.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	state := strings.ToLower(a.State)
	var err error
	switch state {
	case "", "visible":
		err = chromedp.Run(wctx, chromedp.WaitVisible(sel, chromedp.ByQuery))
	case "attached":
		err = chromedp.Run(wctx, chromedp.WaitReady(sel, chromedp.ByQuery))
	case "detached":
		err = chromedp.Run(wctx, chromedp.WaitNotPresent(sel, chromedp.ByQuery))
	default:
		return &mcpservers.BrowserResult{Error: fmt.Sprintf("wait_for_selector: unknown state %q (visible|attached|detached)", a.State)}, nil
	}
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: fmt.Sprintf("state=%s", state)}, nil
}

func (s *Service) doKeyboardPress(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	if a.Key == "" {
		return &mcpservers.BrowserResult{Error: "keyboard_press: key required"}, nil
	}
	actions := []chromedp.Action{}
	if a.Selector != "" {
		actions = append(actions, chromedp.Focus(a.Selector, chromedp.ByQuery))
	}
	actions = append(actions, chromedp.KeyEvent(keyFor(a.Key)))
	if err := chromedp.Run(ctx, actions...); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: "pressed=" + a.Key}, nil
}

// keyFor maps a public DOM-event key name to chromedp/kb's string
// constant. Unknown keys are passed through verbatim so callers can
// use letter/digit keys directly.
func keyFor(k string) string {
	switch strings.ToLower(k) {
	case "enter", "return":
		return kb.Enter
	case "tab":
		return kb.Tab
	case "escape", "esc":
		return kb.Escape
	case "backspace":
		return kb.Backspace
	case "delete":
		return kb.Delete
	case "arrowup", "up":
		return kb.ArrowUp
	case "arrowdown", "down":
		return kb.ArrowDown
	case "arrowleft", "left":
		return kb.ArrowLeft
	case "arrowright", "right":
		return kb.ArrowRight
	case "home":
		return kb.Home
	case "end":
		return kb.End
	case "pageup":
		return kb.PageUp
	case "pagedown":
		return kb.PageDown
	}
	return k
}

func (s *Service) doClipboardRead(ctx context.Context) (*mcpservers.BrowserResult, error) {
	var v string
	err := chromedp.Run(ctx, chromedp.Evaluate("navigator.clipboard.readText()", &v))
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: v}, nil
}

func (s *Service) doClipboardWrite(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	script := fmt.Sprintf("navigator.clipboard.writeText(%s)", jsString(a.Text))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, nil)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true}, nil
}

func (s *Service) doCookiesGet(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	// Use CDP Network.getCookies so HttpOnly cookies are visible too.
	// devtools.installListeners already calls network.Enable on
	// EnsureReady; if the listener install failed, NetworkEnable here
	// is a safe no-op retry.
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		_ = network.Enable().Do(c) // idempotent
		var inner error
		cookies, inner = network.GetCookies().Do(c)
		return inner
	}))
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	out := filterCookies(cookies, a.Name, a.Domain)
	raw, _ := json.Marshal(out)
	return &mcpservers.BrowserResult{Success: true, Data: string(raw)}, nil
}

// filterCookies applies the name+domain filters identically to live's
// chrome.cookies.getAll path so all three modes return the same
// shape and selection.
type cookieView struct {
	Name           string  `json:"name"`
	Value          string  `json:"value"`
	Domain         string  `json:"domain"`
	Path           string  `json:"path"`
	Secure         bool    `json:"secure"`
	HTTPOnly       bool    `json:"httpOnly"`
	Session        bool    `json:"session"`
	SameSite       string  `json:"sameSite,omitempty"`
	ExpirationDate float64 `json:"expirationDate,omitempty"`
}

func filterCookies(cookies []*network.Cookie, wantName, wantDomain string) []cookieView {
	out := make([]cookieView, 0, len(cookies))
	for _, c := range cookies {
		if wantName != "" && c.Name != wantName {
			continue
		}
		if wantDomain != "" {
			cd := strings.TrimPrefix(c.Domain, ".")
			if cd != wantDomain && !strings.HasSuffix(wantDomain, "."+cd) && !strings.HasSuffix(cd, "."+wantDomain) {
				continue
			}
		}
		out = append(out, cookieView{
			Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
			Secure: c.Secure, HTTPOnly: c.HTTPOnly, Session: c.Session,
			SameSite:       string(c.SameSite),
			ExpirationDate: c.Expires,
		})
	}
	return out
}

func (s *Service) doStorageGet(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	kind := strings.ToLower(a.Storage)
	if kind == "" {
		kind = "local"
	}
	if kind != "local" && kind != "session" {
		return &mcpservers.BrowserResult{Error: fmt.Sprintf("storage_get: unknown storage %q (local|session)", a.Storage)}, nil
	}
	js := `(function(){
  var s = ` + kind + `Storage;
  var key = ` + jsString(a.StorageKey) + `;
  if (key) return JSON.stringify({key:key, value:s.getItem(key)});
  var out = {};
  for (var i=0; i<s.length; i++) { var k = s.key(i); out[k] = s.getItem(k); }
  return JSON.stringify(out);
})()`
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: raw}, nil
}

func (s *Service) doCapabilities() (*mcpservers.BrowserResult, error) {
	caps := map[string]any{
		"mode":    mcpservers.ModeProbe,
		"methods": probeMethods,
	}
	raw, _ := json.Marshal(caps)
	return &mcpservers.BrowserResult{Success: true, Data: string(raw)}, nil
}

// probeMethods is the static dispatch table this backend supports.
// Exposed via doCapabilities so foreign agents can probe before
// using an action that isn't wired.
var probeMethods = []string{
	mcpservers.ActionNavigate,
	mcpservers.ActionClick,
	mcpservers.ActionType,
	mcpservers.ActionScroll,
	mcpservers.ActionScreenshot,
	mcpservers.ActionExtract,
	mcpservers.ActionBack,
	mcpservers.ActionTabs,
	mcpservers.ActionEvaluate,
	mcpservers.ActionPerfStart,
	mcpservers.ActionPerfStop,
	mcpservers.ActionNetworkList,
	mcpservers.ActionConsoleGet,
	mcpservers.ActionLighthouse,
	mcpservers.ActionWaitForSelector,
	mcpservers.ActionKeyboardPress,
	mcpservers.ActionClipboardRead,
	mcpservers.ActionClipboardWrite,
	mcpservers.ActionCookiesGet,
	mcpservers.ActionStorageGet,
	mcpservers.ActionCapabilities,
}

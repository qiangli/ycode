// Package solo is ycode's "solo" browser mode — chromedp launches a
// fresh isolated Chrome. Tries the host Chrome first; falls back to
// a podman-managed Chromium image so the mode works in environments
// without a host install (CI, server-side).
package solo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/probe"
)

// Config bundles the launch options for solo mode.
type Config struct {
	ChromePath  string // empty → auto-detect; falls back to podman Chromium image
	Headed      bool   // false → headless
	UserDataDir string // empty → temp dir per session
}

// Service is the solo-mode backend.
type Service struct {
	cfg Config

	mu        sync.Mutex
	allocCtx  context.Context
	allocStop context.CancelFunc
	ctx       context.Context
	ctxStop   context.CancelFunc

	tempUserData string // cleaned up on Stop if we created it
}

func New(cfg Config) *Service { return &Service{cfg: cfg} }

func (s *Service) Name() string { return mcpservers.ModeSolo }
func (s *Service) Cfg() Config  { return s.cfg }

// Available reports whether a usable Chrome path exists.
// Phase 3 ships the host-Chrome path; the podman fallback is wired
// but only kicks in if no host Chrome is found AND
// PodmanChromiumImage() returns a valid image reference. The image
// pull/build itself is deferred to a follow-up.
func (s *Service) Available(ctx context.Context) bool {
	if s.cfg.ChromePath != "" {
		_, err := os.Stat(s.cfg.ChromePath)
		return err == nil
	}
	if probe.DetectChrome() != "" {
		return true
	}
	// Podman fallback not yet pullable; surface false for Phase 3.
	return false
}

func (s *Service) EnsureReady(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx != nil {
		return nil
	}

	chrome := s.cfg.ChromePath
	if chrome == "" {
		chrome = probe.DetectChrome()
	}
	if chrome == "" {
		return errors.New("solo: no Chrome on host; podman Chromium fallback not yet implemented (Phase 3 follow-up)")
	}

	userData := s.cfg.UserDataDir
	if userData == "" {
		d, err := os.MkdirTemp("", "ycode-solo-*")
		if err != nil {
			return fmt.Errorf("solo: temp user-data-dir: %w", err)
		}
		userData = d
		s.tempUserData = d
	} else {
		if err := os.MkdirAll(userData, 0o755); err != nil {
			return fmt.Errorf("solo: user-data-dir: %w", err)
		}
	}

	opts := chromedp.DefaultExecAllocatorOptions[:]
	opts = append(opts,
		chromedp.ExecPath(chrome),
		chromedp.UserDataDir(userData),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
	)
	if s.cfg.Headed {
		opts = append(opts, chromedp.Flag("headless", false))
	} else {
		opts = append(opts, chromedp.Headless)
	}

	allocCtx, allocStop := chromedp.NewExecAllocator(context.Background(), opts...)
	cdpCtx, cdpStop := chromedp.NewContext(allocCtx)

	if err := chromedp.Run(cdpCtx); err != nil {
		cdpStop()
		allocStop()
		if s.tempUserData != "" {
			_ = os.RemoveAll(s.tempUserData)
			s.tempUserData = ""
		}
		return fmt.Errorf("solo: launch %s: %w", chrome, err)
	}

	s.allocCtx, s.allocStop = allocCtx, allocStop
	s.ctx, s.ctxStop = cdpCtx, cdpStop
	slog.Info("solo: launched", "chrome", chrome, "headed", s.cfg.Headed, "userDataDir", userData)
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
	if s.tempUserData != "" {
		_ = os.RemoveAll(s.tempUserData)
		s.tempUserData = ""
	}
	return nil
}

func (s *Service) Execute(ctx context.Context, action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	s.mu.Lock()
	cdpCtx := s.ctx
	s.mu.Unlock()
	if cdpCtx == nil {
		return nil, errors.New("solo: not ready (call EnsureReady first)")
	}

	callCtx, cancel := context.WithTimeout(cdpCtx, 30*time.Second)
	defer cancel()

	// Solo and probe share the chromedp action vocabulary; reuse
	// the probe service's dispatch by emulating BrowserAction
	// translation here. To keep packages independent we duplicate
	// the small switch rather than import-cycling.
	switch action.Type {
	case mcpservers.ActionNavigate:
		return runNavigate(callCtx, action.URL)
	case mcpservers.ActionClick:
		return runClick(callCtx, action)
	case mcpservers.ActionType:
		return runType(callCtx, action.Selector, action.Text)
	case mcpservers.ActionScroll:
		return runScroll(callCtx, action.Direction, action.Amount)
	case mcpservers.ActionScreenshot:
		return runScreenshot(callCtx, action)
	case mcpservers.ActionExtract:
		return runExtract(callCtx, action)
	case mcpservers.ActionBack:
		return runBack(callCtx)
	case mcpservers.ActionEvaluate:
		return runEvaluate(callCtx, action.Script)
	case mcpservers.ActionWaitForSelector:
		return runWaitForSelector(callCtx, action)
	case mcpservers.ActionKeyboardPress:
		return runKeyboardPress(callCtx, action)
	case mcpservers.ActionClipboardRead:
		return runClipboardRead(callCtx)
	case mcpservers.ActionClipboardWrite:
		return runClipboardWrite(callCtx, action)
	case mcpservers.ActionCookiesGet:
		return runCookiesGet(callCtx, action)
	case mcpservers.ActionStorageGet:
		return runStorageGet(callCtx, action)
	case mcpservers.ActionCapabilities:
		return runCapabilities()
	}
	return &mcpservers.BrowserResult{
		Error: fmt.Sprintf("solo: action %q not supported", action.Type),
	}, nil
}

// --- chromedp helpers (same shape as probe/, kept duplicated to
// avoid an import cycle and to let probe and solo diverge over time).

func runNavigate(ctx context.Context, url string) (*mcpservers.BrowserResult, error) {
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

func runClick(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	if a.Selector != "" {
		if err := chromedp.Run(ctx, chromedp.Click(a.Selector, chromedp.ByQuery)); err != nil {
			return &mcpservers.BrowserResult{Error: err.Error()}, nil
		}
		return &mcpservers.BrowserResult{Success: true}, nil
	}
	// match_text fallback — walk DOM for an element whose visible
	// text / aria-label / value contains the requested substring
	// (case-insensitive), then click the first match. Identical
	// semantics to live's runInTab(click) match_text path.
	if a.MatchText != "" {
		text := a.MatchText
		js := `(function(){
  var want = ` + jsString(text) + `.toLowerCase();
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

func runType(ctx context.Context, sel, text string) (*mcpservers.BrowserResult, error) {
	if sel == "" {
		return &mcpservers.BrowserResult{Error: "type: selector required"}, nil
	}
	if err := chromedp.Run(ctx, chromedp.SendKeys(sel, text, chromedp.ByQuery)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true}, nil
}

func runScroll(ctx context.Context, direction string, amount int) (*mcpservers.BrowserResult, error) {
	if amount == 0 {
		amount = 500
	}
	if direction == "up" {
		amount = -amount
	}
	script := fmt.Sprintf("window.scrollBy(0, %d); window.scrollY", amount)
	var y float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &y)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: fmt.Sprintf("scrollY=%g", y)}, nil
}

func runScreenshot(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
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

func runExtract(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	var title, url string
	if err := chromedp.Run(ctx, chromedp.Title(&title), chromedp.Location(&url)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
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

// extractScript / parseExtractPayload — duplicated from probe to
// keep packages independent (probe and solo intentionally don't
// import each other beyond the chrome-detect helper). Keep the
// implementations byte-equivalent.
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
  var body = (document.body && document.body.innerText) || "";
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

func runBack(ctx context.Context) (*mcpservers.BrowserResult, error) {
	if err := chromedp.Run(ctx, chromedp.NavigateBack()); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true}, nil
}

func runEvaluate(ctx context.Context, script string) (*mcpservers.BrowserResult, error) {
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

// --- Phase 3–5 actions (same shapes as probe; duplicated to keep
//     packages independent and let solo diverge over time).

func runWaitForSelector(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
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

func runKeyboardPress(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
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

func runClipboardRead(ctx context.Context) (*mcpservers.BrowserResult, error) {
	var v string
	if err := chromedp.Run(ctx, chromedp.Evaluate("navigator.clipboard.readText()", &v)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: v}, nil
}

func runClipboardWrite(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	script := fmt.Sprintf("navigator.clipboard.writeText(%s)", jsString(a.Text))
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, nil)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true}, nil
}

func runCookiesGet(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	// CDP Network.getCookies so HttpOnly cookies are visible (parity
	// with live's chrome.cookies.getAll path).
	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		_ = network.Enable().Do(c)
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

// filterCookies mirrors probe.filterCookies. Kept duplicated to avoid
// the probe→solo cross-import (mcpservers/solo intentionally limits
// its probe import to chrome detection).
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

func runStorageGet(ctx context.Context, a mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
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

func runCapabilities() (*mcpservers.BrowserResult, error) {
	caps := map[string]any{
		"mode":    mcpservers.ModeSolo,
		"methods": soloMethods,
	}
	raw, _ := json.Marshal(caps)
	return &mcpservers.BrowserResult{Success: true, Data: string(raw)}, nil
}

var soloMethods = []string{
	mcpservers.ActionNavigate,
	mcpservers.ActionClick,
	mcpservers.ActionType,
	mcpservers.ActionScroll,
	mcpservers.ActionScreenshot,
	mcpservers.ActionExtract,
	mcpservers.ActionBack,
	mcpservers.ActionEvaluate,
	mcpservers.ActionWaitForSelector,
	mcpservers.ActionKeyboardPress,
	mcpservers.ActionClipboardRead,
	mcpservers.ActionClipboardWrite,
	mcpservers.ActionCookiesGet,
	mcpservers.ActionStorageGet,
	mcpservers.ActionCapabilities,
}

// PodmanChromiumImage returns the OCI tag of the Chromium image used
// when no host Chrome is found. The actual pull/build is deferred to
// a follow-up commit; this constant + the helper below are the seam
// where it lands. See internal/runtime/containertool for the pattern.
const PodmanChromiumImage = "docker.io/chromedp/headless-shell:latest"

// PodmanChromiumFallbackDir is the persistent volume mount inside the
// container for the user-data dir.
const PodmanChromiumFallbackDir = "/profile"

// resolveChromePath resolves the Chrome binary to use. Exported for
// diagnostics from `ycode browser doctor`. Returns "" if nothing
// usable is found.
func (s *Service) ResolveChromePath() string {
	if s.cfg.ChromePath != "" {
		return s.cfg.ChromePath
	}
	return probe.DetectChrome()
}

// DefaultUserDataDir is what `ycode browser doctor` shows as the
// default solo profile location when none is configured.
func DefaultUserDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "ycode", "solo-profile")
}

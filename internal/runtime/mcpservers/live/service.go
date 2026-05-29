// Package live is ycode's "live" browser mode — a ycode-owned MV3
// Chrome extension paired with a Go WebSocket server, used to drive
// the user's real, logged-in Chrome (cookies, SSO, fingerprint).
//
// The server side (this package) binds 127.0.0.1:<port> (default
// 58082) and waits for the extension to connect. Once connected,
// every BrowserAction is translated into a JSON request, sent over
// WebSocket, and the response is unmarshaled back into a
// BrowserResult.
//
// The extension source lives under ./extension/ and is bundled into
// the binary via go:embed. `ycode browser setup live` extracts the
// files so the user can load them via chrome://extensions →
// Developer mode → Load unpacked.
package live

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// DefaultPort is the well-known loopback port the live extension
// connects to. Override via settings.json `browser.livePort`.
const DefaultPort = 58082

// LiveExtensionMinVersion is the minimum acceptable extension version
// for full feature coverage. The hub refuses to dispatch to any older
// version with an actionable "extension stale, reload at
// chrome://extensions" error. The version is reported by the
// extension's `_hello` frame on connect. 0.4.0 added the
// chrome.debugger permission for trusted keystrokes; 0.5.0 added
// network_list / console_get / perf_start / perf_stop / lighthouse.
const LiveExtensionMinVersion = "0.5.0"

// LiveHandshakeTimeout caps how long hub.call waits for the
// extension to send its _hello frame before treating the connection
// as too old (pre-0.4.0 extensions don't send _hello).
const LiveHandshakeTimeout = 3 * time.Second

// roleKind selects how a Service routes BrowserActions. A single
// Service either owns the hub locally (roleHub) or forwards every
// call to a hub already running in another ycode process (roleClient).
type roleKind int

const (
	roleUnset  roleKind = iota
	roleHub             // this process binds 127.0.0.1:<port> and owns the WS to the extension
	roleClient          // another ycode process owns the hub; we POST /dispatch
)

// Service is the live-mode backend. Two roles share one type so
// callers don't have to know which one is active.
type Service struct {
	port int

	mu   sync.Mutex
	role roleKind
	hub  *hub         // populated when role == roleHub
	http *http.Client // populated when role == roleClient
}

// New returns a live-mode service.
func New(port int) *Service {
	if port == 0 {
		port = DefaultPort
	}
	return &Service{port: port}
}

func (s *Service) Name() string { return mcpservers.ModeLive }
func (s *Service) Port() int    { return s.port }

func (s *Service) Available(ctx context.Context) bool {
	// Live mode is "available" once we either own the hub or can
	// see one in another process. The extension's WS may or may
	// not be connected yet (doctor surfaces the distinction).
	return true
}

// EnsureReady picks a role based on whether the live port is in use:
//
//   - port free → we bind the hub locally (typical for `ycode serve`,
//     and for `ycode prompt` when no serve is running)
//   - port in use → another ycode process already owns the hub
//     (typically `ycode serve`). We switch to client role and forward
//     every Execute to it via HTTP POST /dispatch.
func (s *Service) EnsureReady(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.role != roleUnset {
		return nil
	}
	if portInUse(s.port) {
		// Confirm it's a live hub (not some unrelated service) by
		// pinging /health. If it doesn't answer, fall through to a
		// real bind so the user gets a useful error.
		if probeHealth(s.port) {
			s.role = roleClient
			s.http = &http.Client{Timeout: 35 * time.Second}
			slog.Info("live: hub already owned by another ycode process; using client role", "port", s.port)
			return nil
		}
	}
	h := newHub(s.port)
	if err := h.start(ctx); err != nil {
		return err
	}
	s.role = roleHub
	s.hub = h
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.role {
	case roleHub:
		err := s.hub.stop(ctx)
		s.hub = nil
		s.role = roleUnset
		return err
	case roleClient:
		s.http = nil
		s.role = roleUnset
	}
	return nil
}

// Connected reports whether the extension is currently attached. In
// client role we ask the owner's /health endpoint; in hub role we
// check directly.
func (s *Service) Connected() bool {
	s.mu.Lock()
	role := s.role
	hub := s.hub
	s.mu.Unlock()
	switch role {
	case roleHub:
		return hub != nil && hub.connected()
	case roleClient:
		return probeHealth(s.port)
	}
	return false
}

// portInUse returns true when a TCP listen on 127.0.0.1:port fails
// because someone else holds the port.
func portInUse(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

// probeHealth GETs http://127.0.0.1:port/health with a tight timeout.
// Used to confirm the port-holding process is a ycode-live hub
// (and not some other service squatting on 58082).
func probeHealth(port int) bool {
	c := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (s *Service) Execute(ctx context.Context, action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	s.mu.Lock()
	role := s.role
	hub := s.hub
	client := s.http
	s.mu.Unlock()

	method, params, err := actionToParams(action)
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}

	// Hard staleness + per-method gate. Only meaningful in roleHub —
	// in roleClient the hub-owning process will run the same checks
	// when it receives the /dispatch POST. Allow `capabilities` through
	// so `ycode browser doctor` can still introspect a stale extension.
	if role == roleHub && hub != nil && action.Type != mcpservers.ActionCapabilities {
		if ver := hub.ExtVersion(); ver == "" {
			// Conn is up but no _hello yet. awaitHello inside hub.call
			// will surface the timeout error; nothing to do here.
		} else if versionLess(ver, LiveExtensionMinVersion) {
			return &mcpservers.BrowserResult{Error: staleExtensionError(ver)}, nil
		} else if methods := hub.ExtMethods(); len(methods) > 0 && !slices.Contains(methods, method) {
			return &mcpservers.BrowserResult{Error: methodNotAdvertisedError(method, ver)}, nil
		}
	}

	// Wait-for-selector callers pass timeout_ms; respect it as the
	// outer deadline (plus a small buffer) so the call doesn't time
	// out before the extension's internal poll.
	timeout := 30 * time.Second
	if action.TimeoutMs > 0 {
		t := time.Duration(action.TimeoutMs)*time.Millisecond + 5*time.Second
		if t > timeout {
			timeout = t
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if action.Type == mcpservers.ActionCapabilities {
		slog.Info("live: capabilities probed", "role", role, "callers", captureCallers(2, 8))
	}

	var res *mcpservers.BrowserResult
	switch role {
	case roleHub:
		res, err = s.executeHub(callCtx, hub, method, params)
	case roleClient:
		res, err = s.executeClient(callCtx, client, method, params)
	default:
		return nil, errors.New("live: not ready (call EnsureReady first)")
	}
	if res != nil {
		// Post-process: screenshot save-to-file (cap + spill), record
		// last tab URL, prepend stale-extension hint.
		s.postprocess(action, res, hub)
	}
	return res, err
}

// postprocess applies cross-cutting transforms to a fresh result:
//   - screenshot MaxBytes/SavePath enforcement,
//   - last-tab URL recording (for the "not connected" error).
//
// Staleness/per-method enforcement happens upstream in Execute as a
// hard short-circuit, so we no longer surface a hint here.
func (s *Service) postprocess(action mcpservers.BrowserAction, res *mcpservers.BrowserResult, h *hub) {
	if action.Type == mcpservers.ActionScreenshot && res.Image != "" {
		if action.MaxBytes > 0 || action.SavePath != "" {
			raw, err := decodeB64(res.Image)
			if err == nil {
				img, path, err := mcpservers.PostprocessScreenshot(raw, action)
				if err == nil {
					res.Image = img
					res.Path = path
				}
			}
		}
	}
	if h != nil && res.URL != "" {
		h.RecordLastTab(res.URL)
	}
}

// captureCallers walks the stack starting `skip` frames above the
// caller and returns up to `max` "package.Function:line" strings.
// Used by the capabilities-probe log to attribute the originator
// without dumping a full debug.Stack().
func captureCallers(skip, max int) []string {
	pcs := make([]uintptr, max)
	n := runtime.Callers(skip+1, pcs)
	if n == 0 {
		return nil
	}
	frames := runtime.CallersFrames(pcs[:n])
	out := make([]string, 0, n)
	for {
		f, more := frames.Next()
		out = append(out, fmt.Sprintf("%s:%d", f.Function, f.Line))
		if !more {
			break
		}
	}
	return out
}

// staleExtensionError builds the actionable message for an extension
// that's connected but reports a version below LiveExtensionMinVersion.
func staleExtensionError(ver string) string {
	return fmt.Sprintf("live: extension stale (v%s < required v%s). "+
		"Reload it at chrome://extensions, or run: ycode browser install-extension",
		ver, LiveExtensionMinVersion)
}

// methodNotAdvertisedError fires when the extension's _hello methods
// list omits the method we want to dispatch. This is the direct cure
// for the original incident: an old extension at the required version
// floor that doesn't actually implement a new method.
func methodNotAdvertisedError(method, ver string) string {
	return fmt.Sprintf("live: method %q not advertised by extension v%s. "+
		"Reload it at chrome://extensions, or run: ycode browser install-extension",
		method, ver)
}

func (s *Service) executeHub(ctx context.Context, h *hub, method string, params map[string]any) (*mcpservers.BrowserResult, error) {
	resp, err := h.call(ctx, method, params)
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	if resp.Error != "" {
		return &mcpservers.BrowserResult{Error: resp.Error}, nil
	}
	return unmarshalExt(resp.Result)
}

func (s *Service) executeClient(ctx context.Context, c *http.Client, method string, params map[string]any) (*mcpservers.BrowserResult, error) {
	body, err := json.Marshal(map[string]any{"method": method, "params": params})
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/dispatch", s.port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return &mcpservers.BrowserResult{Error: fmt.Sprintf("live: dispatch to hub: %v", err)}, nil
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return &mcpservers.BrowserResult{Error: fmt.Sprintf("live: hub returned %d: %s", resp.StatusCode, string(rawBody))}, nil
	}
	var dispatchResp struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(rawBody, &dispatchResp); err != nil {
		return &mcpservers.BrowserResult{Error: fmt.Sprintf("live: bad dispatch payload: %v", err)}, nil
	}
	if dispatchResp.Error != "" {
		return &mcpservers.BrowserResult{Error: dispatchResp.Error}, nil
	}
	return unmarshalExt(dispatchResp.Result)
}

func unmarshalExt(raw json.RawMessage) (*mcpservers.BrowserResult, error) {
	var inner extResult
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &inner); err != nil {
			return &mcpservers.BrowserResult{Error: fmt.Sprintf("live: bad result payload: %v", err)}, nil
		}
	}
	return &mcpservers.BrowserResult{
		Success:   true,
		Title:     inner.Title,
		URL:       inner.URL,
		Content:   inner.Content,
		Elements:  inner.Elements,
		Data:      inner.Data,
		Image:     inner.Image,
		Path:      inner.Path,
		Total:     inner.Total,
		Truncated: inner.Truncated,
	}, nil
}

// decodeB64 strips the data-URL prefix the extension may emit and
// returns raw bytes. Live's takeScreenshot strips the prefix already
// but this is defence-in-depth for older builds.
func decodeB64(s string) ([]byte, error) {
	if idx := strings.Index(s, ","); idx >= 0 && strings.HasPrefix(s, "data:") {
		s = s[idx+1:]
	}
	return base64.StdEncoding.DecodeString(s)
}

// versionLess returns true when a < b using a simple
// dotted-numeric comparison. Any non-numeric segment is treated as 0.
// Sufficient for our manifest versions (X.Y.Z).
func versionLess(a, b string) bool {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var an, bn int
		if i < len(as) {
			an, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bn, _ = strconv.Atoi(bs[i])
		}
		if an < bn {
			return true
		}
		if an > bn {
			return false
		}
	}
	return false
}

// actionToParams translates a BrowserAction into a {method, params}
// pair for the WebSocket protocol. Keep this list in sync with the
// extension's background.js dispatch table.
func actionToParams(a mcpservers.BrowserAction) (string, map[string]any, error) {
	switch a.Type {
	case mcpservers.ActionNavigate:
		return "navigate", map[string]any{"url": a.URL}, nil
	case mcpservers.ActionClick:
		return "click", map[string]any{
			"selector":   a.Selector,
			"element_id": a.ElementID,
			"match_text": a.MatchText,
			"scope":      a.Scope,
		}, nil
	case mcpservers.ActionType:
		return "type", map[string]any{"selector": a.Selector, "element_id": a.ElementID, "text": a.Text}, nil
	case mcpservers.ActionScroll:
		return "scroll", map[string]any{"direction": a.Direction, "amount": a.Amount}, nil
	case mcpservers.ActionScreenshot:
		return "screenshot", map[string]any{}, nil
	case mcpservers.ActionExtract:
		return "extract", map[string]any{
			"goal":       a.Goal,
			"match_text": a.MatchText,
			"scope":      a.Scope,
			"limit":      a.Limit,
			"offset":     a.Offset,
		}, nil
	case mcpservers.ActionBack:
		return "back", map[string]any{}, nil
	case mcpservers.ActionTabs:
		return "tabs", map[string]any{"action": a.TabAction, "tab_id": a.TabID}, nil
	case mcpservers.ActionEvaluate:
		return "evaluate", map[string]any{"script": a.Script}, nil
	case mcpservers.ActionWaitForSelector:
		return "wait_for_selector", map[string]any{
			"selector":   a.Selector,
			"timeout_ms": a.TimeoutMs,
			"state":      a.State,
		}, nil
	case mcpservers.ActionKeyboardPress:
		return "keyboard_press", map[string]any{
			"key":       a.Key,
			"modifiers": a.Modifiers,
			"selector":  a.Selector,
		}, nil
	case mcpservers.ActionClipboardRead:
		return "clipboard_read", map[string]any{}, nil
	case mcpservers.ActionClipboardWrite:
		return "clipboard_write", map[string]any{"text": a.Text}, nil
	case mcpservers.ActionCookiesGet:
		return "cookies_get", map[string]any{"name": a.Name, "domain": a.Domain}, nil
	case mcpservers.ActionStorageGet:
		return "storage_get", map[string]any{"storage": a.Storage, "key": a.StorageKey}, nil
	case mcpservers.ActionCapabilities:
		return "capabilities", map[string]any{}, nil
	case mcpservers.ActionNetworkList:
		return "network_list", map[string]any{}, nil
	case mcpservers.ActionConsoleGet:
		return "console_get", map[string]any{}, nil
	case mcpservers.ActionPerfStart:
		return "perf_start", map[string]any{}, nil
	case mcpservers.ActionPerfStop:
		return "perf_stop", map[string]any{}, nil
	case mcpservers.ActionLighthouse:
		return "lighthouse", map[string]any{}, nil
	}
	return "", nil, fmt.Errorf("live: action %q not supported", a.Type)
}

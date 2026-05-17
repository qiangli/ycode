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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// DefaultPort is the well-known loopback port the live extension
// connects to. Override via settings.json `browser.livePort`.
const DefaultPort = 58082

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

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	switch role {
	case roleHub:
		return s.executeHub(callCtx, hub, method, params)
	case roleClient:
		return s.executeClient(callCtx, client, method, params)
	}
	return nil, errors.New("live: not ready (call EnsureReady first)")
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
		Success:  true,
		Title:    inner.Title,
		URL:      inner.URL,
		Content:  inner.Content,
		Elements: inner.Elements,
		Data:     inner.Data,
		Image:    inner.Image,
	}, nil
}

// actionToParams translates a BrowserAction into a {method, params}
// pair for the WebSocket protocol. Keep this list in sync with the
// extension's background.js dispatch table.
func actionToParams(a mcpservers.BrowserAction) (string, map[string]any, error) {
	switch a.Type {
	case mcpservers.ActionNavigate:
		return "navigate", map[string]any{"url": a.URL}, nil
	case mcpservers.ActionClick:
		return "click", map[string]any{"selector": a.Selector, "element_id": a.ElementID}, nil
	case mcpservers.ActionType:
		return "type", map[string]any{"selector": a.Selector, "element_id": a.ElementID, "text": a.Text}, nil
	case mcpservers.ActionScroll:
		return "scroll", map[string]any{"direction": a.Direction, "amount": a.Amount}, nil
	case mcpservers.ActionScreenshot:
		return "screenshot", map[string]any{}, nil
	case mcpservers.ActionExtract:
		return "extract", map[string]any{"goal": a.Goal}, nil
	case mcpservers.ActionBack:
		return "back", map[string]any{}, nil
	case mcpservers.ActionTabs:
		return "tabs", map[string]any{"action": a.TabAction, "tab_id": a.TabID}, nil
	case mcpservers.ActionEvaluate:
		return "evaluate", map[string]any{"script": a.Script}, nil
	}
	return "", nil, fmt.Errorf("live: action %q not supported", a.Type)
}

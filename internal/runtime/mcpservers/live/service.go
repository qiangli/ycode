//go:build experimental

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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// DefaultPort is the well-known loopback port the live extension
// connects to. Override via settings.json `browser.livePort`.
const DefaultPort = 58082

// Service is the live-mode backend.
type Service struct {
	port int

	mu   sync.Mutex
	hub  *hub
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
	// Live mode is "available" once the Go server is up; the
	// extension may or may not be connected yet (doctor surfaces
	// the distinction).
	return true
}

func (s *Service) EnsureReady(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hub != nil {
		return nil
	}
	h := newHub(s.port)
	if err := h.start(ctx); err != nil {
		return err
	}
	s.hub = h
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hub == nil {
		return nil
	}
	err := s.hub.stop(ctx)
	s.hub = nil
	return err
}

// Connected reports whether the extension is currently attached.
func (s *Service) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hub != nil && s.hub.connected()
}

func (s *Service) Execute(ctx context.Context, action mcpservers.BrowserAction) (*mcpservers.BrowserResult, error) {
	s.mu.Lock()
	h := s.hub
	s.mu.Unlock()
	if h == nil {
		return nil, errors.New("live: not ready (call EnsureReady first)")
	}

	method, params, err := actionToParams(action)
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := h.call(callCtx, method, params)
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	if resp.Error != "" {
		return &mcpservers.BrowserResult{Error: resp.Error}, nil
	}
	var inner extResult
	if len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, &inner); err != nil {
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
	}
	return "", nil, fmt.Errorf("live: action %q not supported", a.Type)
}

// Package probe is ycode's "probe" browser mode — pure-Go CDP attach
// to a Chrome started with --remote-debugging-port. Drives real
// DevTools data (perf traces, network waterfalls, source-mapped
// console). Built on chromedp.
package probe

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/chromedp/chromedp"

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
		return s.doScreenshot(callCtx)
	case mcpservers.ActionExtract:
		return s.doExtract(callCtx)
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
	sel := a.Selector
	if sel == "" {
		return &mcpservers.BrowserResult{Error: "click: selector required"}, nil
	}
	if err := chromedp.Run(ctx, chromedp.Click(sel, chromedp.ByQuery)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true}, nil
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

func (s *Service) doScreenshot(ctx context.Context) (*mcpservers.BrowserResult, error) {
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{
		Success: true,
		Image:   base64.StdEncoding.EncodeToString(buf),
	}, nil
}

func (s *Service) doExtract(ctx context.Context) (*mcpservers.BrowserResult, error) {
	var title, url, body string
	err := chromedp.Run(ctx,
		chromedp.Title(&title),
		chromedp.Location(&url),
		chromedp.Text("body", &body, chromedp.NodeVisible),
	)
	if err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{
		Success: true,
		Title:   title,
		URL:     url,
		Content: truncate(body, 32000),
	}, nil
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

//go:build experimental

// Package solo is ycode's "solo" browser mode — chromedp launches a
// fresh isolated Chrome. Tries the host Chrome first; falls back to
// a podman-managed Chromium image so the mode works in environments
// without a host install (CI, server-side).
package solo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/chromedp"

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
		return runClick(callCtx, action.Selector)
	case mcpservers.ActionType:
		return runType(callCtx, action.Selector, action.Text)
	case mcpservers.ActionScroll:
		return runScroll(callCtx, action.Direction, action.Amount)
	case mcpservers.ActionScreenshot:
		return runScreenshot(callCtx)
	case mcpservers.ActionExtract:
		return runExtract(callCtx)
	case mcpservers.ActionBack:
		return runBack(callCtx)
	case mcpservers.ActionEvaluate:
		return runEvaluate(callCtx, action.Script)
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

func runClick(ctx context.Context, sel string) (*mcpservers.BrowserResult, error) {
	if sel == "" {
		return &mcpservers.BrowserResult{Error: "click: selector required"}, nil
	}
	if err := chromedp.Run(ctx, chromedp.Click(sel, chromedp.ByQuery)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{Success: true}, nil
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

func runScreenshot(ctx context.Context) (*mcpservers.BrowserResult, error) {
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
		return &mcpservers.BrowserResult{Error: err.Error()}, nil
	}
	return &mcpservers.BrowserResult{
		Success: true,
		Image:   base64.StdEncoding.EncodeToString(buf),
	}, nil
}

func runExtract(ctx context.Context) (*mcpservers.BrowserResult, error) {
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

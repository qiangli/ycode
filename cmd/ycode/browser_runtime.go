//go:build experimental

package main

import (
	"context"
	"log/slog"

	"github.com/qiangli/ycode/internal/runtime/browser"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/mcpservers"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/live"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/probe"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/reliability"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/solo"
	"github.com/qiangli/ycode/internal/tools"
)

// setupBrowserBackend installs the configured browser mode (live,
// probe, or solo) and returns a browser.Client that dispatches
// wire.Actions through the manager. Returns nil when browser.mode is
// unset; callers install the client on their rootCtx via
// browser.WithClient.
//
// Stable builds use the stub in browser_runtime_stub.go and always
// return nil.
func setupBrowserBackend(ctx context.Context, cfg *config.Config) browser.Client {
	if cfg == nil || cfg.Browser == nil || cfg.Browser.Mode == "" {
		return nil
	}
	mode := cfg.Browser.Mode
	mgr := mcpservers.NewManager()

	var svc mcpservers.Service
	switch mode {
	case mcpservers.ModeLive:
		svc = live.New(cfg.Browser.LivePort)
	case mcpservers.ModeProbe:
		svc = probe.New(cfg.Browser.ProbeURL)
	case mcpservers.ModeSolo:
		svc = solo.New(solo.Config{
			ChromePath:  cfg.Browser.SoloChromePath,
			Headed:      cfg.Browser.SoloHeaded,
			UserDataDir: cfg.Browser.SoloUserDataDir,
		})
	default:
		slog.Warn("browser: unknown mode", "mode", mode)
		return nil
	}

	// Wrap with the openchrome-inspired reliability primitives.
	svc = reliability.Wrap(svc, reliability.Config{
		HintEngine:     cfg.Browser.HintEngine,
		RalphFallback:  cfg.Browser.RalphFallback,
		CircuitBreaker: cfg.Browser.CircuitBreaker,
		CompactDOM:     cfg.Browser.CompactDOM,
		PatternLearner: cfg.Browser.PatternLearner,
	})
	mgr.Register(svc)

	// live mode owns a long-lived WS listener that the Chrome
	// extension reconnects to. Bind it eagerly so the extension
	// can connect the moment ycode starts, rather than waiting
	// for the first browser_* tool call (the lazy path).
	// probe and solo are lazy-initialized — they need user
	// action (`ycode browser launch` / a configured Chrome) that
	// might not be ready at session start.
	if mode == mcpservers.ModeLive {
		if err := svc.EnsureReady(ctx); err != nil {
			slog.Warn("browser: live EnsureReady failed", "error", err)
		}
	}
	if err := mgr.SetDefault(mode); err != nil {
		slog.Warn("browser: SetDefault failed", "error", err)
		return nil
	}
	slog.Info("browser: backend installed", "mode", mode)
	return tools.NewInprocClient(mgr)
}

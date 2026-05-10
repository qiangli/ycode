//go:build experimental

package main

import (
	"context"
	"log/slog"

	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/mcpservers"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/live"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/probe"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/reliability"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/solo"
	"github.com/qiangli/ycode/internal/tools"
)

// setupBrowserBackend installs the configured browser mode (live,
// probe, or solo) into the browser_* tool dispatch path. No-op when
// browser.mode is unset. Stable builds bypass via the stub below.
func setupBrowserBackend(ctx context.Context, cfg *config.Config) {
	if cfg == nil || cfg.Browser == nil || cfg.Browser.Mode == "" {
		return
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
		return
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
	if err := mgr.SetDefault(mode); err != nil {
		slog.Warn("browser: SetDefault failed", "error", err)
		return
	}
	tools.SetBrowserManager(mgr)
	slog.Info("browser: backend installed", "mode", mode)
}

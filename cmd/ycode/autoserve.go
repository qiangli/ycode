package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/qiangli/ycode/internal/observability"
)

// maybeStartOTELServer checks if a server is already running at the configured port.
// If not, it auto-starts one in background goroutines.
// Returns the stack manager (if started) and whether this instance started the server.
func maybeStartOTELServer(ctx context.Context) (*observability.StackManager, bool) {
	// Check if server is already running.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", otelPort)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(healthURL)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			slog.Info("otel: connected to existing server", "port", otelPort)
			return nil, false
		}
	}

	// No server running — start one.
	slog.Info("otel: no server found, auto-starting", "port", otelPort)

	cfg, dataDir, err := loadServeConfig()
	if err != nil {
		slog.Warn("otel: failed to load config for auto-start", "error", err)
		return nil, false
	}
	cfg.ProxyPort = otelPort

	mgr := buildStackManager(cfg, dataDir)
	if err := mgr.Start(ctx); err != nil {
		slog.Warn("otel: auto-start failed", "error", err)
		return nil, false
	}

	slog.Info("otel: auto-started server", "port", otelPort)
	return mgr, true
}

// stopAutoStartedServer shuts down the auto-started observability server on exit.
func stopAutoStartedServer(mgr *observability.StackManager) {
	slog.Info("otel: stopping auto-started server")
	if err := mgr.Stop(context.Background()); err != nil {
		slog.Warn("otel: stop server", "error", err)
	}
}

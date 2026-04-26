package inference

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync/atomic"
)

// OllamaComponent implements the observability.Component interface for
// the embedded Ollama inference engine. It manages the external C++ runner
// binary and provides a reverse-proxy HTTP handler for the Ollama API.
type OllamaComponent struct {
	cfg     *Config
	dataDir string
	runner  *RunnerManager
	healthy atomic.Bool
	otel    *otelState // nil if OTEL not configured
}

// NewOllamaComponent creates a component that manages the Ollama inference engine.
// dataDir is the directory for model storage and runtime data.
func NewOllamaComponent(cfg *Config, dataDir string) *OllamaComponent {
	return &OllamaComponent{
		cfg:     cfg,
		dataDir: dataDir,
	}
}

func (o *OllamaComponent) Name() string { return "ollama" }

func (o *OllamaComponent) Start(ctx context.Context) error {
	// Ensure data directory exists.
	if err := os.MkdirAll(o.dataDir, 0o755); err != nil {
		return fmt.Errorf("ollama: create data dir: %w", err)
	}

	// Set OLLAMA_MODELS if custom models directory is configured.
	if o.cfg.ModelsDir != "" {
		os.Setenv("OLLAMA_MODELS", o.cfg.ModelsDir)
	}

	// Create and start the runner manager.
	runner, err := NewRunnerManager(o.cfg)
	if err != nil {
		return fmt.Errorf("ollama: %w", err)
	}
	o.runner = runner

	// Wire OTEL callbacks for crash/restart tracing.
	runner.OnCrash = func(crashErr error) {
		o.traceRunnerCrash(ctx, crashErr)
		o.updateOTELGauges()
	}
	runner.OnRestart = func() {
		o.traceRunnerStart(ctx)
		o.updateOTELGauges()
	}

	if err := runner.Start(ctx); err != nil {
		return fmt.Errorf("ollama: start runner: %w", err)
	}

	o.healthy.Store(true)
	o.traceRunnerStart(ctx)
	o.updateOTELGauges()
	slog.Info("ollama: component started", "port", runner.Port(), "data", o.dataDir)
	return nil
}

func (o *OllamaComponent) Stop(ctx context.Context) error {
	o.healthy.Store(false)
	o.updateOTELGauges()
	if o.runner != nil {
		return o.runner.Stop(ctx)
	}
	return nil
}

func (o *OllamaComponent) Healthy() bool {
	return o.healthy.Load() && o.runner != nil && o.runner.Healthy()
}

// HTTPHandler returns a reverse proxy to the Ollama runner's HTTP API.
// Mounted at /ollama/ on the proxy landing page.
func (o *OllamaComponent) HTTPHandler() http.Handler {
	if o.runner == nil || o.runner.Port() == 0 {
		return nil
	}
	target, _ := url.Parse(o.runner.BaseURL())
	return httputil.NewSingleHostReverseProxy(target)
}

// Port returns the Ollama runner's HTTP port for reverse proxying.
func (o *OllamaComponent) Port() int {
	if o.runner == nil {
		return 0
	}
	return o.runner.Port()
}

// BaseURL returns the full Ollama API base URL.
func (o *OllamaComponent) BaseURL() string {
	if o.runner == nil {
		return ""
	}
	return o.runner.BaseURL()
}

// Runner returns the underlying runner manager for direct access.
func (o *OllamaComponent) Runner() *RunnerManager {
	return o.runner
}

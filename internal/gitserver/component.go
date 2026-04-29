package gitserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// GitServerComponent implements observability.Component for the embedded Gitea git server.
type GitServerComponent struct {
	server  *Server
	dataDir string
	cfg     *ComponentConfig
	healthy atomic.Bool
	otel    *gitOTELState
}

// ComponentConfig holds configuration for the git server component.
type ComponentConfig struct {
	Enabled  bool   `json:"enabled"`
	DataDir  string `json:"dataDir,omitempty"`
	AppName  string `json:"appName,omitempty"`
	HTTPOnly bool   `json:"httpOnly,omitempty"`
	Token    string `json:"token,omitempty"`
	SubPath  string `json:"-"` // URL sub-path for reverse proxy (e.g. "/git/"), set by serve
}

// gitOTELState holds OTEL instruments for the git server.
type gitOTELState struct {
	tracer trace.Tracer
	meter  metric.Meter
}

// NewGitServerComponent creates a new git server component.
func NewGitServerComponent(cfg *ComponentConfig, dataDir string) *GitServerComponent {
	if cfg.AppName == "" {
		cfg.AppName = "ycode Git"
	}
	return &GitServerComponent{
		cfg:     cfg,
		dataDir: dataDir,
	}
}

// Name returns the component name.
func (g *GitServerComponent) Name() string { return "git" }

// Start launches the embedded Gitea server.
func (g *GitServerComponent) Start(ctx context.Context) error {
	slog.Info("gitserver: starting component")

	server, err := NewServer(&ServerConfig{
		DataDir:  g.dataDir,
		AppName:  g.cfg.AppName,
		HTTPOnly: g.cfg.HTTPOnly,
		Token:    g.cfg.Token,
		SubPath:  g.cfg.SubPath,
	})
	if err != nil {
		return fmt.Errorf("gitserver: init: %w", err)
	}

	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("gitserver: start: %w", err)
	}

	g.server = server
	g.healthy.Store(true)

	if g.otel != nil && g.otel.tracer != nil {
		_, span := g.otel.tracer.Start(ctx, "ycode.gitserver.start",
			trace.WithAttributes(
				attribute.Int("gitserver.port", server.Port()),
				attribute.String("gitserver.data_dir", g.dataDir),
			),
		)
		span.End()
	}

	slog.Info("gitserver: component started", "port", server.Port(), "data", g.dataDir)
	return nil
}

// Stop shuts down the git server.
func (g *GitServerComponent) Stop(ctx context.Context) error {
	g.healthy.Store(false)
	if g.server != nil {
		return g.server.Stop(ctx)
	}
	return nil
}

// Healthy returns true if the git server is operational.
func (g *GitServerComponent) Healthy() bool {
	return g.healthy.Load() && g.server != nil && g.server.Healthy()
}

// HTTPHandler returns nil — Gitea is accessed via reverse proxy (Port())
// so the proxy preserves the /git/ path prefix that Gitea expects.
func (g *GitServerComponent) HTTPHandler() http.Handler {
	return nil
}

// Port returns the server's HTTP port.
func (g *GitServerComponent) Port() int {
	if g.server == nil {
		return 0
	}
	return g.server.Port()
}

// BaseURL returns the server's base URL.
func (g *GitServerComponent) BaseURL() string {
	if g.server == nil {
		return ""
	}
	return g.server.BaseURL()
}

// SetOTEL configures OTEL instrumentation for the git server.
func (g *GitServerComponent) SetOTEL(tracer trace.Tracer, meter metric.Meter) {
	g.otel = &gitOTELState{tracer: tracer, meter: meter}
}

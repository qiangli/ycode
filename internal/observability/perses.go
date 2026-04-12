package observability

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	persesembed "github.com/perses/perses/embed"
	"github.com/perses/perses/pkg/model/api/config"
)

// PersesComponent runs Perses in-process as a goroutine for dashboards.
type PersesComponent struct {
	port           int
	prometheusAddr string // address of embedded Prometheus (e.g. "127.0.0.1:9090")
	dataDir        string
	pathPrefix     string // proxy path prefix (e.g. "/dashboard")

	server  *persesembed.Server
	healthy atomic.Bool
}

// NewPersesComponent creates an in-process Perses component.
func NewPersesComponent(port int, prometheusAddr, dataDir string) *PersesComponent {
	return &PersesComponent{
		port:           port,
		prometheusAddr: prometheusAddr,
		dataDir:        dataDir,
	}
}

func (p *PersesComponent) Name() string             { return "perses" }
func (p *PersesComponent) SetPathPrefix(pfx string) { p.pathPrefix = pfx }

func (p *PersesComponent) Start(ctx context.Context) error {
	// Set the listen address flag before starting Perses. The perses/common
	// app.Runner reads this flag to configure the echo HTTP server.
	_ = flag.Set("web.listen-address", fmt.Sprintf(":%d", p.port))

	conf := config.Config{
		APIPrefix: p.pathPrefix,
		Database: config.Database{
			File: &config.File{
				Folder:    p.dataDir + "/data",
				Extension: "json",
			},
		},
	}

	server, err := persesembed.Start(ctx, conf)
	if err != nil {
		return fmt.Errorf("perses: start: %w", err)
	}
	p.server = server
	p.healthy.Store(true)
	slog.Info("perses: started", "port", p.port, "prometheus", p.prometheusAddr)
	return nil
}

func (p *PersesComponent) Stop(_ context.Context) error {
	p.healthy.Store(false)
	if p.server != nil {
		_ = p.server.Stop()
	}
	slog.Info("perses: stopped")
	return nil
}

func (p *PersesComponent) Healthy() bool { return p.healthy.Load() }

// HTTPHandler returns nil — Perses runs its own HTTP server.
func (p *PersesComponent) HTTPHandler() http.Handler { return nil }

// Port returns the Perses HTTP port.
func (p *PersesComponent) Port() int { return p.port }

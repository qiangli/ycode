package observability

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/qiangli/ycode/internal/observability/dashboards"

	persesembed "github.com/perses/perses/embed"
	"github.com/perses/perses/pkg/model/api/config"
	"github.com/perses/perses/pkg/model/api/v1/secret"
)

const persesDefaultKey = "e=dz;`M'5Pjvy^Sq3FVBkTC@N9?H/gua"

// PersesComponent runs Perses in-process as a goroutine for dashboards.
type PersesComponent struct {
	port          int
	prometheusURL string // URL for Prometheus query API (e.g. "http://127.0.0.1:58080/prometheus")
	dataDir       string
	pathPrefix    string // proxy path prefix (e.g. "/dashboard")

	server  *persesembed.Server
	healthy atomic.Bool
}

// NewPersesComponent creates an in-process Perses component.
// prometheusURL is the Prometheus query API endpoint that Perses uses as a datasource.
func NewPersesComponent(port int, prometheusURL, dataDir string) *PersesComponent {
	return &PersesComponent{
		port:          port,
		prometheusURL: prometheusURL,
		dataDir:       dataDir,
	}
}

func (p *PersesComponent) Name() string             { return "perses" }
func (p *PersesComponent) SetPathPrefix(pfx string) { p.pathPrefix = pfx }

func (p *PersesComponent) Start(ctx context.Context) error {
	if !IsPortAvailable(p.port) {
		return fmt.Errorf("perses: port %d already in use", p.port)
	}

	// Set the listen address flag before starting Perses. The perses/common
	// app.Runner reads this flag to configure the echo HTTP server.
	_ = flag.Set("web.listen-address", fmt.Sprintf(":%d", p.port))

	// Seed default projects, datasource, and dashboards directly into the
	// file database. Writing to the DB directory bypasses Perses's plugin
	// schema validation (which requires plugin archives we don't ship).
	dbDir := p.dataDir + "/data"
	if err := dashboards.Provision(dbDir, p.prometheusURL); err != nil {
		slog.Warn("perses: seeding dashboards failed", "error", err)
	}

	// Configure plugin paths. Perses loads UI plugins (TimeSeriesChart,
	// StatChart, PrometheusTimeSeriesQuery, etc.) from archive files.
	// Run scripts/fetch-perses-plugins.sh to download them.
	pluginDir := filepath.Join(p.dataDir, "plugins")
	archiveDir := filepath.Join(p.dataDir, "plugins-archive")
	_ = os.MkdirAll(pluginDir, 0o755)
	_ = os.MkdirAll(archiveDir, 0o755)

	conf := config.Config{
		APIPrefix: p.pathPrefix,
		Database: config.Database{
			File: &config.File{
				Folder:    dbDir,
				Extension: "json",
			},
		},
		Security: config.Security{
			EncryptionKey: secret.Hidden(persesDefaultKey),
		},
		Plugin: config.Plugin{
			Path:         pluginDir,
			ArchivePaths: []string{archiveDir},
		},
	}
	// Verify() validates and hex-encodes the encryption key before use.
	if err := conf.Security.Verify(); err != nil {
		return fmt.Errorf("perses: verify security config: %w", err)
	}

	server, err := persesembed.Start(ctx, conf)
	if err != nil {
		return fmt.Errorf("perses: start: %w", err)
	}
	p.server = server
	p.healthy.Store(true)
	slog.Info("perses: started", "port", p.port, "prometheus", p.prometheusURL)
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

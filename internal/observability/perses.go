package observability

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/qiangli/ycode/internal/observability/dashboards"
	"github.com/qiangli/ycode/internal/observability/plugins"

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
	pluginDir := filepath.Join(p.dataDir, "plugins")
	archiveDir := filepath.Join(p.dataDir, "plugins-archive")

	// Always re-provision from embedded archives to ensure runtime plugins
	// match the binary. Stale plugins from previous builds cause version
	// mismatch warnings and load errors.
	_ = os.RemoveAll(pluginDir)
	_ = os.RemoveAll(archiveDir)
	_ = os.MkdirAll(pluginDir, 0o755)
	_ = os.MkdirAll(archiveDir, 0o755)
	provisionPluginArchives(archiveDir)

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

// provisionPluginArchives extracts embedded plugin archives to destDir.
// Falls back to copying from known filesystem locations if no archives
// were embedded at build time.
func provisionPluginArchives(destDir string) {
	// 1. Try embedded archives (self-contained binary).
	if n := extractEmbeddedPlugins(destDir); n > 0 {
		slog.Info("perses: extracted embedded plugin archives", "count", n)
		return
	}

	// 2. Fallback: copy from filesystem (legacy path or manual fetch).
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".ycode", "observability", "perses", "plugins-archive"),
	}
	for _, src := range candidates {
		if n := copyPluginArchives(src, destDir); n > 0 {
			slog.Info("perses: provisioned plugin archives", "source", src, "count", n)
			return
		}
	}

	slog.Warn("perses: no plugin archives found — dashboards will lack chart rendering. " +
		"Run: make init && make compile")
}

// extractEmbeddedPlugins writes embedded .tar.gz archives to destDir.
// Returns the number of archives extracted.
func extractEmbeddedPlugins(destDir string) int {
	entries, err := plugins.ArchiveFS.ReadDir("archive")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		data, err := plugins.ArchiveFS.ReadFile("archive/" + e.Name())
		if err != nil {
			continue
		}
		if os.WriteFile(filepath.Join(destDir, e.Name()), data, 0o644) == nil {
			n++
		}
	}
	return n
}

// copyPluginArchives copies .tar.gz files from srcDir to destDir.
// Returns the number of files copied.
func copyPluginArchives(srcDir, destDir string) int {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			continue
		}
		if os.WriteFile(filepath.Join(destDir, e.Name()), data, 0o644) == nil {
			n++
		}
	}
	return n
}

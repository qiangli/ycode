package observability

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/qiangli/ycode/pkg/otel/victorialogs/httpserver"
	"github.com/qiangli/ycode/pkg/otel/victorialogs/insertutil"
	"github.com/qiangli/ycode/pkg/otel/victorialogs/vlinsert"
	"github.com/qiangli/ycode/pkg/otel/victorialogs/vlselect"
	"github.com/qiangli/ycode/pkg/otel/victorialogs/vlstorage"
)

// VictoriaLogsComponent runs VictoriaLogs in-process as a goroutine.
// Receives logs from the OTEL Collector via OTLP HTTP.
type VictoriaLogsComponent struct {
	port       int
	dataDir    string
	pathPrefix string // proxy path prefix (e.g. "/logs")
	healthy    atomic.Bool
	cancel     context.CancelFunc
}

// NewVictoriaLogsComponent creates an in-process VictoriaLogs component.
func NewVictoriaLogsComponent(port int, dataDir string) *VictoriaLogsComponent {
	return &VictoriaLogsComponent{port: port, dataDir: dataDir}
}

func (v *VictoriaLogsComponent) Name() string           { return "victoria-logs" }
func (v *VictoriaLogsComponent) SetPathPrefix(p string) { v.pathPrefix = p }

func (v *VictoriaLogsComponent) Start(ctx context.Context) error {
	// Check port availability BEFORE starting. VictoriaLogs' httpserver calls
	// logger.Fatalf (→ os.Exit) on bind failure, which would kill the entire process.
	if !IsPortAvailable(v.port) {
		return fmt.Errorf("victoria-logs: port %d already in use", v.port)
	}

	ctx, cancel := context.WithCancel(ctx)
	v.cancel = cancel

	// Configure VictoriaLogs via flags (its native config mechanism).
	_ = flag.Set("storageDataPath", v.dataDir+"/data")
	if v.pathPrefix != "" {
		_ = flag.Set("http.pathPrefix", v.pathPrefix)
	}
	// VictoriaMetrics libraries require flag.Parse() to have been called
	// before accessing memory limits and other flag-dependent values.
	// Parse with empty args to avoid re-parsing os.Args, which contains
	// cobra flags unknown to stdlib flag (flag.Parse would call os.Exit(2)).
	if !flag.Parsed() {
		flag.CommandLine.Parse([]string{})
	}

	// Clean up stale lock file left by a killed process.
	cleanStaleFlock(v.dataDir + "/data")

	// Initialize storage, select, and insert subsystems.
	vlstorage.Init()
	vlselect.Init()
	insertutil.SetLogRowsStorage(&vlstorage.Storage{})
	vlinsert.Init()

	// Start HTTP server in a goroutine.
	listenAddrs := []string{fmt.Sprintf("127.0.0.1:%d", v.port)}
	go httpserver.Serve(listenAddrs, v.requestHandler, httpserver.ServeOptions{})

	v.healthy.Store(true)
	slog.Info("victoria-logs: started", "port", v.port, "dataDir", v.dataDir)

	// Watch for context cancellation to shut down.
	go func() {
		<-ctx.Done()
		_ = httpserver.Stop(listenAddrs)
		vlinsert.Stop()
		vlselect.Stop()
		vlstorage.Stop()
		v.healthy.Store(false)
		slog.Info("victoria-logs: stopped")
	}()

	return nil
}

func (v *VictoriaLogsComponent) Stop(_ context.Context) error {
	if v.cancel != nil {
		v.cancel()
	}
	return nil
}

func (v *VictoriaLogsComponent) Healthy() bool { return v.healthy.Load() }

// HTTPHandler returns nil — VictoriaLogs runs its own httpserver.
// Accessed via reverse proxy from the stack manager.
func (v *VictoriaLogsComponent) HTTPHandler() http.Handler { return nil }

// Port returns the allocated HTTP port.
func (v *VictoriaLogsComponent) Port() int { return v.port }

// requestHandler delegates to VictoriaLogs subsystems.
func (v *VictoriaLogsComponent) requestHandler(w http.ResponseWriter, r *http.Request) bool {
	if vlinsert.RequestHandler(w, r) {
		return true
	}
	if vlselect.RequestHandler(w, r) {
		return true
	}
	if vlstorage.RequestHandler(w, r) {
		return true
	}
	// Redirect root to the built-in vmui web interface.
	if r.URL.Path == "/" || r.URL.Path == "" {
		target := "/select/vmui/"
		if v.pathPrefix != "" {
			target = v.pathPrefix + target
		}
		http.Redirect(w, r, target, http.StatusFound)
		return true
	}
	return false
}

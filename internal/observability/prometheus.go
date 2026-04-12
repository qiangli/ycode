package observability

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/tsdb"
)

//go:embed static/prometheus
var prometheusUI embed.FS

// PrometheusComponent provides an embedded Prometheus TSDB and PromQL engine.
// Runs entirely in-process as goroutines.
type PrometheusComponent struct {
	dataDir          string
	scrapeTargetAddr string // collector's prometheus exporter (e.g. "127.0.0.1:8889")
	pathPrefix       string // proxy path prefix (e.g. "/prometheus")

	mu         sync.Mutex
	db         *tsdb.DB
	engine     *promql.Engine
	server     *http.Server
	listenAddr string
	port       int
	healthy    atomic.Bool
	cancel     context.CancelFunc
}

// NewPrometheusComponent creates an embedded Prometheus component.
func NewPrometheusComponent(dataDir, scrapeTargetAddr string) *PrometheusComponent {
	return &PrometheusComponent{
		dataDir:          dataDir,
		scrapeTargetAddr: scrapeTargetAddr,
	}
}

func (p *PrometheusComponent) Name() string             { return "prometheus" }
func (p *PrometheusComponent) SetPathPrefix(pfx string) { p.pathPrefix = pfx }

func (p *PrometheusComponent) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tsdbDir := filepath.Join(p.dataDir, "data")
	if err := os.MkdirAll(tsdbDir, 0o755); err != nil {
		return fmt.Errorf("prometheus: create data dir: %w", err)
	}

	db, err := tsdb.Open(tsdbDir, nil, nil, &tsdb.Options{
		RetentionDuration: int64(15 * 24 * time.Hour / time.Millisecond),
		MinBlockDuration:  int64(2 * time.Hour / time.Millisecond),
		MaxBlockDuration:  int64(36 * time.Hour / time.Millisecond),
	}, nil)
	if err != nil {
		return fmt.Errorf("prometheus: open tsdb: %w", err)
	}
	p.db = db

	p.engine = promql.NewEngine(promql.EngineOpts{
		MaxSamples:    50000000,
		Timeout:       2 * time.Minute,
		LookbackDelta: 5 * time.Minute,
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		db.Close()
		return fmt.Errorf("prometheus: listen: %w", err)
	}
	p.listenAddr = listener.Addr().String()
	p.port = listener.Addr().(*net.TCPAddr).Port

	// Serve the embedded Prometheus web UI and API endpoints.
	// The reverse proxy forwards the full path including the prefix
	// (e.g. /prometheus/api/v1/query), so we mount under the prefix.
	uiFS, _ := fs.Sub(prometheusUI, "static/prometheus")
	prefix := p.pathPrefix // e.g. "/prometheus"

	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/api/v1/query", p.handleQuery)
	mux.HandleFunc(prefix+"/api/v1/query_range", p.handleQueryRange)
	mux.HandleFunc(prefix+"/-/healthy", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})
	// Serve embedded UI static files under the prefix.
	fileServer := http.StripPrefix(prefix, http.FileServer(http.FS(uiFS)))
	mux.Handle(prefix+"/", fileServer)

	p.server = &http.Server{Handler: mux}
	go func() {
		if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("prometheus: serve failed", "error", err)
		}
	}()

	scrapeCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	if p.scrapeTargetAddr != "" {
		go p.scrapeLoop(scrapeCtx)
	}

	p.healthy.Store(true)
	slog.Info("prometheus: started", "tsdb", tsdbDir, "api", p.listenAddr)
	return nil
}

func (p *PrometheusComponent) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.healthy.Store(false)
	if p.cancel != nil {
		p.cancel()
	}
	if p.server != nil {
		_ = p.server.Shutdown(ctx)
	}
	if p.db != nil {
		_ = p.db.Close()
	}
	slog.Info("prometheus: stopped")
	return nil
}

func (p *PrometheusComponent) Healthy() bool { return p.healthy.Load() }

// HTTPHandler returns nil — Prometheus runs its own HTTP server.
// Accessed via reverse proxy from the stack manager.
func (p *PrometheusComponent) HTTPHandler() http.Handler { return nil }

// Port returns the Prometheus HTTP port for reverse proxying.
func (p *PrometheusComponent) Port() int { return p.port }

func (p *PrometheusComponent) handleQuery(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	if query == "" {
		http.Error(w, `{"status":"error","error":"missing query"}`, http.StatusBadRequest)
		return
	}
	qry, err := p.engine.NewInstantQuery(r.Context(), p.db, nil, query, time.Now())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"status":"error","error":"%s"}`, err), http.StatusBadRequest)
		return
	}
	defer qry.Close()
	res := qry.Exec(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if res.Err != nil {
		http.Error(w, fmt.Sprintf(`{"status":"error","error":"%s"}`, res.Err), http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, `{"status":"success","data":{"resultType":"%s","result":%s}}`, res.Value.Type(), res.Value.String())
}

func (p *PrometheusComponent) handleQueryRange(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	if query == "" {
		http.Error(w, `{"status":"error","error":"missing query"}`, http.StatusBadRequest)
		return
	}
	end := time.Now()
	start := end.Add(-1 * time.Hour)
	qry, err := p.engine.NewRangeQuery(r.Context(), p.db, nil, query, start, end, 15*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"status":"error","error":"%s"}`, err), http.StatusBadRequest)
		return
	}
	defer qry.Close()
	res := qry.Exec(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if res.Err != nil {
		http.Error(w, fmt.Sprintf(`{"status":"error","error":"%s"}`, res.Err), http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, `{"status":"success","data":{"resultType":"%s","result":%s}}`, res.Value.Type(), res.Value.String())
}

func (p *PrometheusComponent) scrapeLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// TODO: scrape collector /metrics endpoint and write to TSDB
		}
	}
}

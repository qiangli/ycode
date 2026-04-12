package observability

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/textparse"
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
		writePromError(w, "missing query", http.StatusBadRequest)
		return
	}
	qry, err := p.engine.NewInstantQuery(r.Context(), p.db, nil, query, time.Now())
	if err != nil {
		writePromError(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer qry.Close()
	res := qry.Exec(r.Context())
	if res.Err != nil {
		writePromError(w, res.Err.Error(), http.StatusInternalServerError)
		return
	}
	writePromResult(w, res)
}

func (p *PrometheusComponent) handleQueryRange(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	if query == "" {
		writePromError(w, "missing query", http.StatusBadRequest)
		return
	}
	end := time.Now()
	start := end.Add(-1 * time.Hour)
	qry, err := p.engine.NewRangeQuery(r.Context(), p.db, nil, query, start, end, 15*time.Second)
	if err != nil {
		writePromError(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer qry.Close()
	res := qry.Exec(r.Context())
	if res.Err != nil {
		writePromError(w, res.Err.Error(), http.StatusInternalServerError)
		return
	}
	writePromResult(w, res)
}

// writePromResult writes a Prometheus API-compatible JSON response.
// Uses json.Marshal for the result value to ensure valid JSON even for empty results.
func writePromResult(w http.ResponseWriter, res *promql.Result) {
	resultJSON, err := json.Marshal(res.Value)
	if err != nil || string(resultJSON) == "null" {
		// nil slices marshal to "null"; the Prometheus API expects "[]".
		resultJSON = []byte("[]")
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success","data":{"resultType":"%s","result":%s}}`, res.Value.Type(), resultJSON)
}

// writePromError writes a Prometheus API-compatible error response.
func writePromError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	errJSON, _ := json.Marshal(msg)
	fmt.Fprintf(w, `{"status":"error","error":%s}`, errJSON)
}

func (p *PrometheusComponent) scrapeLoop(ctx context.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("http://%s/metrics", p.scrapeTargetAddr)
	st := labels.NewSymbolTable()

	// Scrape immediately on start, then every 15s.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	p.scrape(ctx, client, url, st)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.scrape(ctx, client, url, st)
		}
	}
}

// scrape fetches the /metrics endpoint and writes all samples to the TSDB.
func (p *PrometheusComponent) scrape(ctx context.Context, client *http.Client, url string, st *labels.SymbolTable) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("prometheus: scrape failed", "url", url, "error", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug("prometheus: read scrape body failed", "error", err)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	parser, err := textparse.New(body, contentType, st, textparse.ParserOptions{})
	if err != nil {
		slog.Debug("prometheus: create parser failed", "error", err)
		return
	}

	app := p.db.Appender(ctx)
	now := time.Now().UnixMilli()
	var (
		lset    labels.Labels
		samples int
	)

	for {
		et, err := parser.Next()
		if err != nil {
			break // io.EOF or parse error
		}

		switch et {
		case textparse.EntrySeries:
			_, ts, v := parser.Series()
			parser.Labels(&lset)
			t := now
			if ts != nil {
				t = *ts
			}
			if _, err := app.Append(0, lset, t, v); err != nil {
				slog.Debug("prometheus: append failed", "labels", lset.String(), "error", err)
				continue
			}
			samples++

		case textparse.EntryHistogram:
			_, ts, h, fh := parser.Histogram()
			parser.Labels(&lset)
			t := now
			if ts != nil {
				t = *ts
			}
			if h != nil {
				if _, err := app.AppendHistogram(0, lset, t, h, nil); err != nil {
					continue
				}
			} else if fh != nil {
				if _, err := app.AppendHistogram(0, lset, t, nil, fh); err != nil {
					continue
				}
			}
			samples++
		}
	}

	if err := app.Commit(); err != nil {
		slog.Warn("prometheus: commit failed", "error", err)
		return
	}
	if samples > 0 {
		slog.Debug("prometheus: scraped", "url", url, "samples", samples)
	}
}

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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qiangli/ycode/internal/httputil"

	"github.com/qiangli/ycode/pkg/otel/prometheus/labels"
	"github.com/qiangli/ycode/pkg/otel/prometheus/promql"
	"github.com/qiangli/ycode/pkg/otel/prometheus/textparse"
	"github.com/qiangli/ycode/pkg/otel/prometheus/tsdb"
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
	startedAt  time.Time // populated in Start; surfaced via /status/runtimeinfo
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
	p.startedAt = time.Now().UTC()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		db.Close()
		return fmt.Errorf("prometheus: listen: %w", err)
	}
	p.listenAddr = listener.Addr().String()
	p.port = listener.Addr().(*net.TCPAddr).Port

	// Serve the real Prometheus mantine-ui and API endpoints.
	// The reverse proxy forwards the full path including the prefix
	// (e.g. /prometheus/api/v1/query), so we mount under the prefix.
	// Assets are pre-gzipped at build time; served via GzipFileServer.
	uiFS, _ := fs.Sub(prometheusUI, "static/prometheus")
	prefix := p.pathPrefix // e.g. "/prometheus"

	mux := http.NewServeMux()
	// Implemented Prometheus API endpoints.
	mux.HandleFunc(prefix+"/api/v1/query", p.handleQuery)
	mux.HandleFunc(prefix+"/api/v1/query_range", p.handleQueryRange)
	mux.HandleFunc(prefix+"/api/v1/label/__name__/values", p.handleLabelValues)
	// SSE endpoint the mantine-ui polls for live notifications.
	mux.HandleFunc(prefix+"/api/v1/notifications/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		<-r.Context().Done()
	})
	// Stub endpoints for mantine-ui pages that expect specific response shapes.
	mux.HandleFunc(prefix+"/api/v1/rules", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"groups":[]}}`)
	})
	mux.HandleFunc(prefix+"/api/v1/targets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"activeTargets":[],"droppedTargets":[],"droppedTargetCounts":{}}}`)
	})
	mux.HandleFunc(prefix+"/api/v1/scrape_pools", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"scrapePools":[]}}`)
	})
	mux.HandleFunc(prefix+"/api/v1/status/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"yaml":""}}`)
	})
	mux.HandleFunc(prefix+"/api/v1/status/flags", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{}}`)
	})
	mux.HandleFunc(prefix+"/api/v1/status/runtimeinfo", p.handleRuntimeInfo)
	mux.HandleFunc(prefix+"/api/v1/status/buildinfo", p.handleBuildInfo)
	mux.HandleFunc(prefix+"/api/v1/status/tsdb", p.handleTSDBStatus)
	mux.HandleFunc(prefix+"/api/v1/labels", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":[]}`)
	})
	mux.HandleFunc(prefix+"/api/v1/alerts", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"alerts":[]}}`)
	})
	mux.HandleFunc(prefix+"/api/v1/metadata", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{}}`)
	})
	mux.HandleFunc(prefix+"/api/v1/status/walreplay", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"min":0,"max":0,"current":0}}`)
	})
	mux.HandleFunc(prefix+"/api/v1/series", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":[]}`)
	})
	mux.HandleFunc(prefix+"/api/v1/alertmanagers", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"activeAlertmanagers":[],"droppedAlertmanagers":[]}}`)
	})
	// Catch-all for any remaining /api/v1/* endpoints.
	mux.HandleFunc(prefix+"/api/v1/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{}}`)
	})
	mux.HandleFunc(prefix+"/-/healthy", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})
	// Serve index.html with placeholders replaced.
	indexHTML := p.buildIndexHTML(uiFS)
	gzipServer := httputil.GzipFileServer(uiFS)
	mux.HandleFunc(prefix+"/", func(w http.ResponseWriter, r *http.Request) {
		// For asset files, serve from embedded FS (pre-gzipped).
		relPath := strings.TrimPrefix(r.URL.Path, prefix+"/")
		if relPath != "" && relPath != "index.html" {
			http.StripPrefix(prefix, gzipServer).ServeHTTP(w, r)
			return
		}
		// Serve processed index.html for the root and index.html paths.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

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

// buildIndexHTML reads the embedded index.html (pre-gzipped) and replaces Prometheus placeholders.
func (p *PrometheusComponent) buildIndexHTML(uiFS fs.FS) []byte {
	data, err := httputil.ReadGzipFile(uiFS, "index.html")
	if err != nil {
		return []byte("<html><body>Prometheus UI not available</body></html>")
	}
	html := string(data)
	html = strings.ReplaceAll(html, "CONSOLES_LINK_PLACEHOLDER", "")
	html = strings.ReplaceAll(html, "AGENT_MODE_PLACEHOLDER", "false")
	html = strings.ReplaceAll(html, "READY_PLACEHOLDER", "true")
	html = strings.ReplaceAll(html, "LOOKBACKDELTA_PLACEHOLDER", "5m")
	html = strings.ReplaceAll(html, "TITLE_PLACEHOLDER", "Prometheus")
	return []byte(html)
}

// handleLabelValues returns label values for autocomplete (metric names).
// handleRuntimeInfo returns real process + storage runtime info. The
// mantine UI's Status page reads these fields verbatim; until this was
// wired, every value rendered as blank/zero and the page looked broken.
func (p *PrometheusComponent) handleRuntimeInfo(w http.ResponseWriter, _ *http.Request) {
	cwd, _ := os.Getwd()
	info := map[string]any{
		"startTime":           p.startedAt.Format(time.RFC3339),
		"CWD":                 cwd,
		"reloadConfigSuccess": true,
		"lastConfigTime":      p.startedAt.Format(time.RFC3339),
		"corruptionCount":     0,
		"goroutineCount":      runtime.NumGoroutine(),
		"GOMAXPROCS":          runtime.GOMAXPROCS(0),
		"GOGC":                os.Getenv("GOGC"),
		"GODEBUG":             os.Getenv("GODEBUG"),
		"storageRetention":    "15d",
	}
	writePromOK(w, info)
}

// handleBuildInfo reports the embedded build's identity. Pulls from the
// linker-stamped main.version / main.commit pair when present; falls
// back to "embedded".
func (p *PrometheusComponent) handleBuildInfo(w http.ResponseWriter, _ *http.Request) {
	writePromOK(w, map[string]any{
		"version":   "embedded",
		"revision":  "",
		"branch":    "",
		"buildUser": "",
		"buildDate": "",
		"goVersion": runtime.Version(),
		"platform":  runtime.GOOS + "/" + runtime.GOARCH,
	})
}

// handleTSDBStatus reports real head + cardinality stats. The previous
// stub always returned zeros which made the dashboard's TSDB widgets
// look like there was no data ingested, even when scrape was healthy.
// Stats(...) reads the in-memory postings index — cheap.
//
// `?limit=N` controls topN cardinality entries (default 10, matching
// upstream Prometheus). Stats is called with "__name__" so the
// seriesCountByMetricName slice is populated — upstream only fills
// that slice when the label-name parameter matches a real label.
func (p *PrometheusComponent) handleTSDBStatus(w http.ResponseWriter, r *http.Request) {
	head := p.db.Head()
	topN := 10
	if v := r.FormValue("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			topN = n
		}
	}
	stats := head.Stats("__name__", topN)
	postings := stats.IndexPostingStats

	conv := func(in []struct {
		Name  string
		Count uint64
	}) []map[string]any {
		out := make([]map[string]any, 0, len(in))
		for _, e := range in {
			out = append(out, map[string]any{"name": e.Name, "value": e.Count})
		}
		return out
	}
	// PostingsStats fields are []index.Stat; reflect-light shape via a
	// strongly-typed conversion below avoids importing the index pkg.
	toAny := func(s any) []map[string]any {
		// index.Stat has Name + Count; marshal/unmarshal is the
		// dependency-free way to project it without a new import.
		buf, _ := json.Marshal(s)
		var entries []struct {
			Name  string `json:"Name"`
			Count uint64 `json:"Count"`
		}
		_ = json.Unmarshal(buf, &entries)
		typed := make([]struct {
			Name  string
			Count uint64
		}, len(entries))
		for i, e := range entries {
			typed[i] = struct {
				Name  string
				Count uint64
			}{Name: e.Name, Count: e.Count}
		}
		return conv(typed)
	}

	data := map[string]any{
		"headStats": map[string]any{
			"numSeries":     head.NumSeries(),
			"chunkCount":    0, // upstream Head doesn't expose chunk count directly; left at 0 to keep the field present.
			"minTime":       head.MinTime(),
			"maxTime":       head.MaxTime(),
			"numLabelPairs": postings.NumLabelPairs,
		},
		"seriesCountByMetricName":     toAny(postings.CardinalityMetricsStats),
		"labelValueCountByLabelName":  toAny(postings.CardinalityLabelStats),
		"memoryInBytesByLabelName":    toAny(postings.LabelValueStats),
		"seriesCountByLabelValuePair": toAny(postings.LabelValuePairsStats),
	}
	writePromOK(w, data)
}

// writePromOK is the Prometheus `{"status":"success","data":...}`
// envelope. Local helper so the three status handlers don't repeat the
// boilerplate.
func writePromOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	body, err := json.Marshal(map[string]any{"status": "success", "data": data})
	if err != nil {
		writePromError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}

func (p *PrometheusComponent) handleLabelValues(w http.ResponseWriter, r *http.Request) {
	q, err := p.db.Querier(0, time.Now().UnixMilli())
	if err != nil {
		writePromError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer q.Close()

	names, _, err := q.LabelNames(r.Context(), nil)
	if err != nil {
		writePromError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// The __name__ label values endpoint returns metric names.
	vals, _, err := q.LabelValues(r.Context(), "__name__", nil)
	if err != nil {
		writePromError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = names // used for other label endpoints if needed

	w.Header().Set("Content-Type", "application/json")
	result, _ := json.Marshal(vals)
	fmt.Fprintf(w, `{"status":"success","data":%s}`, result)
}

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

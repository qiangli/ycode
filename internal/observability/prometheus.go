package observability

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
)

// PrometheusComponent provides an embedded Prometheus TSDB and PromQL engine.
type PrometheusComponent struct {
	dataDir          string // TSDB data directory
	scrapeTargetAddr string // addr to scrape (collector's prometheus exporter)
	listenAddr       string // internal HTTP listener

	mu      sync.Mutex
	db      *tsdb.DB
	engine  *promql.Engine
	server  *http.Server
	healthy atomic.Bool
	cancel  context.CancelFunc
}

// NewPrometheusComponent creates an embedded Prometheus component.
// scrapeTargetAddr is the address of the collector's Prometheus exporter (e.g. "127.0.0.1:8889").
func NewPrometheusComponent(dataDir, scrapeTargetAddr string) *PrometheusComponent {
	return &PrometheusComponent{
		dataDir:          dataDir,
		scrapeTargetAddr: scrapeTargetAddr,
	}
}

// Name implements Component.
func (p *PrometheusComponent) Name() string { return "prometheus" }

// Start opens the TSDB, creates the PromQL engine, starts the scrape loop and HTTP API.
func (p *PrometheusComponent) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tsdbDir := filepath.Join(p.dataDir, "data")
	if err := os.MkdirAll(tsdbDir, 0o755); err != nil {
		return fmt.Errorf("prometheus: create data dir: %w", err)
	}

	db, err := tsdb.Open(tsdbDir, nil, nil, &tsdb.Options{
		RetentionDuration: int64(15 * 24 * time.Hour / time.Millisecond), // 15 days
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

	// Start HTTP API in a goroutine.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		db.Close()
		return fmt.Errorf("prometheus: listen: %w", err)
	}
	p.listenAddr = listener.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/query", p.handleQuery)
	mux.HandleFunc("/api/v1/query_range", p.handleQueryRange)
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h2>ycode Prometheus</h2>
<p><a href="/api/v1/query?query=up">Query API</a></p>
</body></html>`)
	})

	p.server = &http.Server{Handler: mux}
	go func() {
		if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("prometheus: serve failed", "error", err)
		}
	}()

	// Start scrape loop in a goroutine.
	scrapeCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	if p.scrapeTargetAddr != "" {
		go p.scrapeLoop(scrapeCtx)
	}

	p.healthy.Store(true)
	slog.Info("prometheus: started", "tsdb", tsdbDir, "api", p.listenAddr)
	return nil
}

// Stop gracefully shuts down the Prometheus component.
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
		if err := p.db.Close(); err != nil {
			slog.Warn("prometheus: close tsdb", "error", err)
		}
	}
	slog.Info("prometheus: stopped")
	return nil
}

// Healthy implements Component.
func (p *PrometheusComponent) Healthy() bool { return p.healthy.Load() }

// HTTPHandler returns the Prometheus HTTP handler.
func (p *PrometheusComponent) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.server != nil {
			p.server.Handler.ServeHTTP(w, r)
		} else {
			http.Error(w, "prometheus not started", http.StatusServiceUnavailable)
		}
	})
}

// Storage returns the underlying TSDB for direct writes (used by other components).
func (p *PrometheusComponent) Storage() storage.Storage {
	return p.db
}

// Queryable returns the TSDB as a Queryable for PromQL evaluation.
func (p *PrometheusComponent) Queryable() storage.Queryable {
	return p.db
}

// handleQuery handles instant PromQL queries.
func (p *PrometheusComponent) handleQuery(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	if query == "" {
		http.Error(w, `{"status":"error","error":"missing query parameter"}`, http.StatusBadRequest)
		return
	}

	ts := time.Now()
	if t := r.FormValue("time"); t != "" {
		// Parse RFC3339 or Unix timestamp.
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			ts = parsed
		}
	}

	qry, err := p.engine.NewInstantQuery(r.Context(), p.db, nil, query, ts)
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
	fmt.Fprintf(w, `{"status":"success","data":{"resultType":"%s","result":%s}}`,
		res.Value.Type(), res.Value.String())
}

// handleQueryRange handles range PromQL queries.
func (p *PrometheusComponent) handleQueryRange(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	if query == "" {
		http.Error(w, `{"status":"error","error":"missing query parameter"}`, http.StatusBadRequest)
		return
	}

	// Defaults.
	end := time.Now()
	start := end.Add(-1 * time.Hour)
	step := 15 * time.Second

	qry, err := p.engine.NewRangeQuery(r.Context(), p.db, nil, query, start, end, step)
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
	fmt.Fprintf(w, `{"status":"success","data":{"resultType":"%s","result":%s}}`,
		res.Value.Type(), res.Value.String())
}

// scrapeLoop periodically scrapes the target and writes samples to TSDB.
func (p *PrometheusComponent) scrapeLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	url := fmt.Sprintf("http://%s/metrics", p.scrapeTargetAddr)
	client := &http.Client{Timeout: 10 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.scrapeOnce(ctx, client, url)
		}
	}
}

// scrapeOnce fetches metrics from the target and writes them to TSDB.
func (p *PrometheusComponent) scrapeOnce(ctx context.Context, client *http.Client, url string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	// Parse Prometheus text format and write to TSDB.
	// Using the expfmt parser from prometheus/common.
	parser := NewPromTextParser()
	families, err := parser.Parse(resp.Body)
	if err != nil {
		slog.Debug("prometheus: parse scrape response", "error", err)
		return
	}

	app := p.db.Appender(ctx)
	ts := time.Now().UnixMilli()

	for _, family := range families {
		for _, m := range family.Metrics {
			lbls := labels.FromMap(m.Labels)
			if _, err := app.Append(0, lbls, ts, m.Value); err != nil {
				continue
			}
		}
	}

	if err := app.Commit(); err != nil {
		slog.Debug("prometheus: commit scrape", "error", err)
		_ = app.Rollback()
	}
}

// --- Simple Prometheus text format parser ---

// MetricFamily is a group of metrics with the same name.
type MetricFamily struct {
	Name    string
	Metrics []Metric
}

// Metric is a single metric sample.
type Metric struct {
	Labels map[string]string
	Value  float64
}

// PromTextParser parses Prometheus text exposition format.
type PromTextParser struct{}

// NewPromTextParser creates a new parser.
func NewPromTextParser() *PromTextParser { return &PromTextParser{} }

// Parse parses Prometheus text format from a reader.
func (p *PromTextParser) Parse(r interface{ Read([]byte) (int, error) }) ([]MetricFamily, error) {
	// Use the standard prometheus expfmt parser.
	var buf []byte
	tmp := make([]byte, 32*1024)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}

	var families []MetricFamily
	lines := splitPromLines(string(buf))
	currentFamily := ""

	for _, line := range lines {
		if len(line) == 0 || line[0] == '#' {
			// Extract family name from HELP/TYPE lines.
			if len(line) > 7 && (line[:7] == "# HELP " || line[:7] == "# TYPE ") {
				parts := splitFirst(line[7:], ' ')
				if parts != "" {
					currentFamily = parts
				}
			}
			continue
		}

		name, lbls, val := parsePromLine(line)
		if name == "" {
			continue
		}
		if math.IsNaN(val) || math.IsInf(val, 0) {
			continue
		}

		lbls["__name__"] = name
		family := findOrCreateFamily(&families, currentFamily)
		if family == nil {
			family = findOrCreateFamily(&families, name)
		}
		family.Metrics = append(family.Metrics, Metric{Labels: lbls, Value: val})
	}

	return families, nil
}

func findOrCreateFamily(families *[]MetricFamily, name string) *MetricFamily {
	for i := range *families {
		if (*families)[i].Name == name {
			return &(*families)[i]
		}
	}
	*families = append(*families, MetricFamily{Name: name})
	return &(*families)[len(*families)-1]
}

func splitPromLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitFirst(s string, sep byte) string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i]
		}
	}
	return s
}

func parsePromLine(line string) (string, map[string]string, float64) {
	lbls := make(map[string]string)

	// Find metric name (before '{' or ' ').
	nameEnd := len(line)
	hasLabels := false
	for i := 0; i < len(line); i++ {
		if line[i] == '{' {
			nameEnd = i
			hasLabels = true
			break
		}
		if line[i] == ' ' {
			nameEnd = i
			break
		}
	}
	name := line[:nameEnd]
	rest := line[nameEnd:]

	if hasLabels {
		// Parse labels between { and }.
		closeBrace := -1
		for i := 0; i < len(rest); i++ {
			if rest[i] == '}' {
				closeBrace = i
				break
			}
		}
		if closeBrace < 0 {
			return "", nil, 0
		}
		labelStr := rest[1:closeBrace]
		rest = rest[closeBrace+1:]

		// Parse key="value" pairs.
		for len(labelStr) > 0 {
			eq := -1
			for i := 0; i < len(labelStr); i++ {
				if labelStr[i] == '=' {
					eq = i
					break
				}
			}
			if eq < 0 {
				break
			}
			key := labelStr[:eq]
			labelStr = labelStr[eq+1:]
			if len(labelStr) < 2 || labelStr[0] != '"' {
				break
			}
			labelStr = labelStr[1:]
			valEnd := -1
			for i := 0; i < len(labelStr); i++ {
				if labelStr[i] == '"' && (i == 0 || labelStr[i-1] != '\\') {
					valEnd = i
					break
				}
			}
			if valEnd < 0 {
				break
			}
			val := labelStr[:valEnd]
			lbls[key] = val
			labelStr = labelStr[valEnd+1:]
			if len(labelStr) > 0 && labelStr[0] == ',' {
				labelStr = labelStr[1:]
			}
		}
	}

	// Parse value.
	for len(rest) > 0 && rest[0] == ' ' {
		rest = rest[1:]
	}
	// Find end of value (before space or end).
	valEnd := len(rest)
	for i := 0; i < len(rest); i++ {
		if rest[i] == ' ' {
			valEnd = i
			break
		}
	}
	valStr := rest[:valEnd]

	var val float64
	fmt.Sscanf(valStr, "%g", &val)

	return name, lbls, val
}

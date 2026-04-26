// Package collector provides an embedded OpenTelemetry Collector.
package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/exporter/debugexporter"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/exporter/otlphttpexporter"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/prometheusexporter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver"
)

// EmbeddedCollector runs an OTEL Collector in-process as a goroutine.
type EmbeddedCollector struct {
	cfg     Config
	dataDir string

	mu      sync.Mutex
	svc     *otelcol.Collector
	healthy atomic.Bool
	cancel  context.CancelFunc
}

// NewEmbeddedCollector creates an embedded collector with the given configuration.
func NewEmbeddedCollector(cfg Config, dataDir string) *EmbeddedCollector {
	return &EmbeddedCollector{
		cfg:     cfg,
		dataDir: dataDir,
	}
}

// Name implements observability.Component.
func (c *EmbeddedCollector) Name() string { return "otel-collector" }

// Start launches the collector in a background goroutine.
func (c *EmbeddedCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Pre-flight port check to fail fast instead of blocking.
	for _, p := range []struct {
		name string
		port int
	}{
		{"gRPC", c.cfg.GRPCPort},
		{"HTTP", c.cfg.HTTPPort},
		{"prometheus", c.cfg.PrometheusPort},
	} {
		if !isPortAvailable(p.port) {
			return fmt.Errorf("collector: %s port %d already in use", p.name, p.port)
		}
	}

	if err := os.MkdirAll(c.dataDir, 0o755); err != nil {
		return fmt.Errorf("create collector data dir: %w", err)
	}
	configYAML := GenerateYAML(c.cfg)

	factories, err := c.factories()
	if err != nil {
		return fmt.Errorf("build collector factories: %w", err)
	}

	settings := otelcol.CollectorSettings{
		BuildInfo: component.BuildInfo{
			Command:     "ycode-collector",
			Description: "Embedded OTEL Collector for ycode",
			Version:     "0.1.0",
		},
		// Prevent the embedded collector from intercepting SIGINT/SIGTERM.
		// Shutdown is handled via context cancellation from the cluster manager.
		DisableGracefulShutdown: true,
		Factories:               func() (otelcol.Factories, error) { return factories, nil },
		ConfigProviderSettings: otelcol.ConfigProviderSettings{
			ResolverSettings: confmap.ResolverSettings{
				URIs:              []string{"yaml:" + configYAML},
				ProviderFactories: []confmap.ProviderFactory{yamlprovider.NewFactory()},
			},
		},
	}

	svc, err := otelcol.NewCollector(settings)
	if err != nil {
		return fmt.Errorf("create collector: %w", err)
	}
	c.svc = svc

	collCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	go func() {
		if err := svc.Run(collCtx); err != nil {
			slog.Error("collector: run failed", "error", err)
		}
		c.healthy.Store(false)
	}()

	c.healthy.Store(true)
	slog.Info("collector: started in-process",
		"grpc", fmt.Sprintf("127.0.0.1:%d", c.cfg.GRPCPort),
		"http", fmt.Sprintf("127.0.0.1:%d", c.cfg.HTTPPort),
	)
	return nil
}

// Stop gracefully shuts down the collector.
// Shutdown() is called first to let exporters drain their queues before the
// context is cancelled — this avoids "connection refused" errors when
// downstream backends (VictoriaLogs, Jaeger) are still running.
func (c *EmbeddedCollector) Stop(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.svc != nil {
		c.svc.Shutdown()
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.healthy.Store(false)
	slog.Info("collector: stopped")
	return nil
}

// Healthy returns true if the collector is running.
func (c *EmbeddedCollector) Healthy() bool {
	return c.healthy.Load()
}

// HTTPHandler returns a handler that serves the status page at the root
// and reverse-proxies OTLP HTTP requests (/v1/traces, /v1/metrics, /v1/logs)
// to the collector's OTLP receiver.
func (c *EmbeddedCollector) HTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// Reverse-proxy OTLP and metrics endpoints to the collector's HTTP receiver.
	otlpTarget, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", c.cfg.HTTPPort))
	otlpProxy := httputil.NewSingleHostReverseProxy(otlpTarget)
	mux.Handle("/v1/", otlpProxy)

	// Reverse-proxy the Prometheus exporter endpoint for metrics scraping.
	promTarget, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", c.cfg.PrometheusPort))
	promProxy := httputil.NewSingleHostReverseProxy(promTarget)
	mux.Handle("/metrics", promProxy)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		healthy := c.Healthy()
		status := "Running"
		statusColor := "#34c759"
		if !healthy {
			status = "Unavailable"
			statusColor = "#ff3b30"
		}

		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>OTEL Collector</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text",sans-serif;
background:#1a1a2e;color:#e0e0e0;padding:32px;min-height:100vh}
h1{font-size:1.5em;font-weight:500;margin-bottom:24px;color:#fff}
.status{display:inline-flex;align-items:center;gap:8px;padding:6px 14px;
border-radius:20px;font-size:13px;font-weight:500;margin-bottom:28px;
background:rgba(255,255,255,0.06)}
.dot{width:8px;height:8px;border-radius:50%%}
.section{margin-bottom:28px}
.section h2{font-size:13px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;
color:rgba(255,255,255,0.5);margin-bottom:12px}
.card{background:rgba(255,255,255,0.05);border:1px solid rgba(255,255,255,0.08);
border-radius:10px;padding:16px;margin-bottom:8px}
.row{display:flex;justify-content:space-between;align-items:center;padding:6px 0;
border-bottom:1px solid rgba(255,255,255,0.05)}
.row:last-child{border-bottom:none}
.label{color:rgba(255,255,255,0.5);font-size:13px}
.value{font-family:SFMono-Regular,Menlo,monospace;font-size:13px;color:#5ac8fa}
.pipeline{display:flex;align-items:center;gap:10px;padding:8px 0;
border-bottom:1px solid rgba(255,255,255,0.05)}
.pipeline:last-child{border-bottom:none}
.pipe-label{font-size:13px;font-weight:500;min-width:64px}
.arrow{color:rgba(255,255,255,0.3);font-size:12px}
.pipe-tag{padding:3px 10px;border-radius:6px;font-size:12px;font-family:monospace;
background:rgba(255,255,255,0.08)}
</style></head><body>
<h1>OTEL Collector</h1>
<div class="status"><div class="dot" style="background:%s"></div>%s</div>

<div class="section"><h2>Receivers</h2><div class="card">
<div class="row"><span class="label">OTLP gRPC</span><span class="value">127.0.0.1:%d</span></div>
<div class="row"><span class="label">OTLP HTTP</span><span class="value">127.0.0.1:%d</span></div>
</div></div>

<div class="section"><h2>Exporters</h2><div class="card">
<div class="row"><span class="label">Prometheus metrics</span><span class="value">127.0.0.1:%d</span></div>`,
			statusColor, status,
			c.cfg.GRPCPort, c.cfg.HTTPPort, c.cfg.PrometheusPort)

		if c.cfg.VictoriaLogsPort > 0 {
			fmt.Fprintf(w, `<div class="row"><span class="label">VictoriaLogs (logs)</span><span class="value">127.0.0.1:%d</span></div>`,
				c.cfg.VictoriaLogsPort)
		}
		if c.cfg.JaegerOTLPPort > 0 {
			fmt.Fprintf(w, `<div class="row"><span class="label">Jaeger OTLP (traces)</span><span class="value">127.0.0.1:%d</span></div>`,
				c.cfg.JaegerOTLPPort)
		}
		if c.cfg.RemoteOTLPEndpoint != "" {
			fmt.Fprintf(w, `<div class="row"><span class="label">Remote OTLP</span><span class="value">%s</span></div>`,
				c.cfg.RemoteOTLPEndpoint)
		}

		fmt.Fprint(w, `</div></div>

<div class="section"><h2>Pipelines</h2><div class="card">
<div class="pipeline"><span class="pipe-label" style="color:#5ac8fa">Metrics</span>
<span class="arrow">&rarr;</span><span class="pipe-tag">otlp</span>
<span class="arrow">&rarr;</span><span class="pipe-tag">batch</span>
<span class="arrow">&rarr;</span><span class="pipe-tag">prometheus</span></div>
<div class="pipeline"><span class="pipe-label" style="color:#af52de">Traces</span>
<span class="arrow">&rarr;</span><span class="pipe-tag">otlp</span>
<span class="arrow">&rarr;</span><span class="pipe-tag">batch</span>
<span class="arrow">&rarr;</span><span class="pipe-tag">otlp/jaeger</span></div>
<div class="pipeline"><span class="pipe-label" style="color:#34c759">Logs</span>
<span class="arrow">&rarr;</span><span class="pipe-tag">otlp</span>
<span class="arrow">&rarr;</span><span class="pipe-tag">batch</span>
<span class="arrow">&rarr;</span><span class="pipe-tag">otlphttp/vlogs</span></div>
</div></div>

<div class="section"><h2>Build</h2><div class="card">
<div class="row"><span class="label">Command</span><span class="value">ycode-collector</span></div>
<div class="row"><span class="label">Version</span><span class="value">0.1.0</span></div>
<div class="row"><span class="label">Processor</span><span class="value">batch (5s)</span></div>
</div></div>
</body></html>`)
	})
	return mux
}

// GRPCAddr returns the collector's gRPC OTLP receiver address.
func (c *EmbeddedCollector) GRPCAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.cfg.GRPCPort)
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// factories builds the component factories for the embedded collector.
func (c *EmbeddedCollector) factories() (otelcol.Factories, error) {
	receivers, err := otelcol.MakeFactoryMap(
		otlpreceiver.NewFactory(),
		hostmetricsreceiver.NewFactory(),
	)
	if err != nil {
		return otelcol.Factories{}, fmt.Errorf("receiver factories: %w", err)
	}

	processors, err := otelcol.MakeFactoryMap(
		batchprocessor.NewFactory(),
	)
	if err != nil {
		return otelcol.Factories{}, fmt.Errorf("processor factories: %w", err)
	}

	exporters, err := otelcol.MakeFactoryMap(
		debugexporter.NewFactory(),
		prometheusexporter.NewFactory(),
		otlpexporter.NewFactory(),     // traces → Jaeger (OTLP gRPC)
		otlphttpexporter.NewFactory(), // logs → VictoriaLogs (OTLP HTTP)
	)
	if err != nil {
		return otelcol.Factories{}, fmt.Errorf("exporter factories: %w", err)
	}

	return otelcol.Factories{
		Receivers:  receivers,
		Processors: processors,
		Exporters:  exporters,
		Telemetry:  otelconftelemetry.NewFactory(),
	}, nil
}

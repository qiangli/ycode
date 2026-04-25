package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/otelcol"

	jaegerembed "github.com/jaegertracing/jaeger/cmd/jaeger/embed"
)

// JaegerComponent runs Jaeger v2 all-in-one in-process as a goroutine.
// Jaeger v2 is built on the OTEL Collector framework with Jaeger-specific
// extensions (jaeger_storage, jaeger_query, remote_sampling).
type JaegerComponent struct {
	otlpPort   int // OTLP gRPC port for receiving traces
	queryPort  int // Jaeger Query UI HTTP port
	dataDir    string
	pathPrefix string // proxy path prefix (e.g. "/traces")

	svc     *otelcol.Collector
	healthy atomic.Bool
	cancel  context.CancelFunc
}

// NewJaegerComponent creates an in-process Jaeger component.
func NewJaegerComponent(otlpPort, queryPort int, dataDir string) *JaegerComponent {
	return &JaegerComponent{otlpPort: otlpPort, queryPort: queryPort, dataDir: dataDir}
}

func (j *JaegerComponent) Name() string           { return "jaeger" }
func (j *JaegerComponent) SetPathPrefix(p string) { j.pathPrefix = p }

func (j *JaegerComponent) Start(ctx context.Context) error {
	// Pre-flight port check to fail fast instead of blocking or crashing.
	for _, p := range []struct {
		name string
		port int
	}{
		{"otlp", j.otlpPort},
		{"query", j.queryPort},
	} {
		if !IsPortAvailable(p.port) {
			return fmt.Errorf("jaeger: %s port %d already in use", p.name, p.port)
		}
	}

	// Build Jaeger all-in-one config YAML programmatically.
	configYAML := j.generateConfig()

	settings := otelcol.CollectorSettings{
		BuildInfo: component.BuildInfo{
			Command:     "ycode-jaeger",
			Description: "Embedded Jaeger for ycode",
			Version:     "0.1.0",
		},
		// Prevent embedded Jaeger from intercepting SIGINT/SIGTERM.
		// Shutdown is handled via context cancellation from the cluster manager.
		DisableGracefulShutdown: true,
		Factories:               jaegerembed.Components,
		ConfigProviderSettings: otelcol.ConfigProviderSettings{
			ResolverSettings: confmap.ResolverSettings{
				URIs:              []string{"yaml:" + configYAML},
				ProviderFactories: []confmap.ProviderFactory{yamlprovider.NewFactory()},
			},
		},
	}

	svc, err := otelcol.NewCollector(settings)
	if err != nil {
		return fmt.Errorf("jaeger: create collector: %w", err)
	}
	j.svc = svc

	collCtx, cancel := context.WithCancel(ctx)
	j.cancel = cancel

	go func() {
		if err := svc.Run(collCtx); err != nil {
			slog.Error("jaeger: run failed", "error", err)
		}
		j.healthy.Store(false)
	}()

	j.healthy.Store(true)
	slog.Info("jaeger: started", "otlp_port", j.otlpPort, "query_port", j.queryPort)
	return nil
}

func (j *JaegerComponent) Stop(_ context.Context) error {
	j.healthy.Store(false)
	if j.cancel != nil {
		j.cancel()
	}
	if j.svc != nil {
		j.svc.Shutdown()
	}
	slog.Info("jaeger: stopped")
	return nil
}

func (j *JaegerComponent) Healthy() bool { return j.healthy.Load() }

// HTTPHandler returns nil — Jaeger runs its own HTTP servers.
// Query UI accessible via reverse proxy.
func (j *JaegerComponent) HTTPHandler() http.Handler { return nil }

// OTLPPort returns the OTLP gRPC port for sending traces.
func (j *JaegerComponent) OTLPPort() int { return j.otlpPort }

// QueryPort returns the Jaeger Query UI HTTP port.
func (j *JaegerComponent) QueryPort() int { return j.queryPort }

// generateConfig produces a minimal Jaeger all-in-one YAML config.
func (j *JaegerComponent) generateConfig() string {
	basePath := j.pathPrefix
	if basePath == "" {
		basePath = "/"
	}
	return fmt.Sprintf(`receivers:
  otlp:
    protocols:
      grpc:
        endpoint: "127.0.0.1:%d"

processors:
  batch:
    timeout: 5s

extensions:
  jaeger_storage:
    backends:
      memstore:
        memory:
          max_traces: 100000
  jaeger_query:
    storage:
      traces: memstore
    base_path: "%s"
    http:
      endpoint: "127.0.0.1:%d"

exporters:
  jaeger_storage_exporter:
    trace_storage: memstore

service:
  telemetry:
    logs:
      level: error
  extensions: [jaeger_storage, jaeger_query]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [jaeger_storage_exporter]
`, j.otlpPort, basePath, j.queryPort)
}

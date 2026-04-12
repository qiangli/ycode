// Package collector provides an embedded OpenTelemetry Collector.
package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/exporter/debugexporter"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/prometheusexporter"
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
		Factories: func() (otelcol.Factories, error) { return factories, nil },
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
func (c *EmbeddedCollector) Stop(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	if c.svc != nil {
		c.svc.Shutdown()
	}
	c.healthy.Store(false)
	slog.Info("collector: stopped")
	return nil
}

// Healthy returns true if the collector is running.
func (c *EmbeddedCollector) Healthy() bool {
	return c.healthy.Load()
}

// HTTPHandler returns a health check handler for the collector.
func (c *EmbeddedCollector) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if c.Healthy() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"status":"unavailable"}`)
		}
	})
	return mux
}

// GRPCAddr returns the collector's gRPC OTLP receiver address.
func (c *EmbeddedCollector) GRPCAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.cfg.GRPCPort)
}

// factories builds the component factories for the embedded collector.
func (c *EmbeddedCollector) factories() (otelcol.Factories, error) {
	receivers, err := otelcol.MakeFactoryMap(
		otlpreceiver.NewFactory(),
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
	)
	if err != nil {
		return otelcol.Factories{}, fmt.Errorf("exporter factories: %w", err)
	}

	return otelcol.Factories{
		Receivers:  receivers,
		Processors: processors,
		Exporters:  exporters,
	}, nil
}

package otel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// ProviderConfig configures the OTEL SDK.
type ProviderConfig struct {
	CollectorAddr  string  // gRPC endpoint, e.g. "127.0.0.1:4317"
	ServiceName    string  // default "ycode"
	ServiceVersion string  // from build ldflags
	SessionID      string  // attached as resource attribute
	InstanceID     string  // unique per ycode process (UUID) for multi-client tracking
	SampleRate     float64 // 1.0 = sample everything

	// File-based persistence.
	DataDir        string // root dir for file exporters (e.g. ~/.ycode/otel)
	PersistTraces  bool
	PersistMetrics bool
}

// Provider holds all three OTEL signal providers.
type Provider struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	Instruments    *Instruments
	shutdownFuncs  []func(context.Context) error
}

// Tracer returns a named tracer from the provider.
func (p *Provider) Tracer(name string) trace.Tracer {
	return p.TracerProvider.Tracer(name)
}

// Meter returns a named meter from the provider.
func (p *Provider) Meter(name string) metric.Meter {
	return p.MeterProvider.Meter(name)
}

// NewProvider creates an OTEL provider with dual export (gRPC + file).
func NewProvider(ctx context.Context, cfg ProviderConfig) (*Provider, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "ycode"
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0
	}

	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		AttrSessionID.String(cfg.SessionID),
	}
	if cfg.InstanceID != "" {
		attrs = append(attrs,
			semconv.ServiceInstanceID(cfg.InstanceID),
			AttrInstanceID.String(cfg.InstanceID),
		)
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	p := &Provider{}

	// --- Trace provider ---
	var spanExporters []sdktrace.SpanExporter

	if cfg.CollectorAddr != "" {
		grpcExp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.CollectorAddr),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, fmt.Errorf("create trace gRPC exporter: %w", err)
		}
		spanExporters = append(spanExporters, grpcExp)
		p.shutdownFuncs = append(p.shutdownFuncs, grpcExp.Shutdown)
	}

	if cfg.PersistTraces && cfg.DataDir != "" {
		fileExp, shutdown, err := newRotatingTraceExporter(cfg.DataDir)
		if err != nil {
			return nil, fmt.Errorf("create trace file exporter: %w", err)
		}
		spanExporters = append(spanExporters, fileExp)
		p.shutdownFuncs = append(p.shutdownFuncs, shutdown)
	}

	var traceOpts []sdktrace.TracerProviderOption
	traceOpts = append(traceOpts, sdktrace.WithResource(res))
	traceOpts = append(traceOpts, sdktrace.WithSampler(
		sdktrace.TraceIDRatioBased(cfg.SampleRate),
	))
	for _, exp := range spanExporters {
		traceOpts = append(traceOpts, sdktrace.WithBatcher(exp))
	}
	p.TracerProvider = sdktrace.NewTracerProvider(traceOpts...)
	p.shutdownFuncs = append(p.shutdownFuncs, p.TracerProvider.Shutdown)
	otel.SetTracerProvider(p.TracerProvider)

	// --- Metric provider ---
	var metricReaders []sdkmetric.Reader

	if cfg.CollectorAddr != "" {
		grpcExp, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(cfg.CollectorAddr),
			otlpmetricgrpc.WithInsecure(),
		)
		if err != nil {
			return nil, fmt.Errorf("create metric gRPC exporter: %w", err)
		}
		metricReaders = append(metricReaders,
			sdkmetric.NewPeriodicReader(grpcExp, sdkmetric.WithInterval(15*time.Second)))
		p.shutdownFuncs = append(p.shutdownFuncs, grpcExp.Shutdown)
	}

	if cfg.PersistMetrics && cfg.DataDir != "" {
		fileExp, shutdown, err := newRotatingMetricExporter(cfg.DataDir)
		if err != nil {
			return nil, fmt.Errorf("create metric file exporter: %w", err)
		}
		metricReaders = append(metricReaders,
			sdkmetric.NewPeriodicReader(fileExp, sdkmetric.WithInterval(30*time.Second)))
		p.shutdownFuncs = append(p.shutdownFuncs, shutdown)
	}

	var meterOpts []sdkmetric.Option
	meterOpts = append(meterOpts, sdkmetric.WithResource(res))
	for _, r := range metricReaders {
		meterOpts = append(meterOpts, sdkmetric.WithReader(r))
	}
	p.MeterProvider = sdkmetric.NewMeterProvider(meterOpts...)
	p.shutdownFuncs = append(p.shutdownFuncs, p.MeterProvider.Shutdown)
	otel.SetMeterProvider(p.MeterProvider)

	// Create pre-built instruments.
	inst, err := NewInstruments(p.MeterProvider.Meter(cfg.ServiceName))
	if err != nil {
		return nil, fmt.Errorf("create instruments: %w", err)
	}
	p.Instruments = inst

	return p, nil
}

// newRotatingTraceExporter creates a file-based trace exporter writing to dataDir/traces/.
func newRotatingTraceExporter(dataDir string) (sdktrace.SpanExporter, func(context.Context) error, error) {
	dir := filepath.Join(dataDir, "traces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	filename := filepath.Join(dir, fmt.Sprintf("traces-%s.jsonl", time.Now().Format("2006-01-02")))
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	exp, err := stdouttrace.New(stdouttrace.WithWriter(f))
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	shutdown := func(ctx context.Context) error {
		if err := exp.Shutdown(ctx); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	return exp, shutdown, nil
}

// newRotatingMetricExporter creates a file-based metric exporter writing to dataDir/metrics/.
func newRotatingMetricExporter(dataDir string) (sdkmetric.Exporter, func(context.Context) error, error) {
	dir := filepath.Join(dataDir, "metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	filename := filepath.Join(dir, fmt.Sprintf("metrics-%s.jsonl", time.Now().Format("2006-01-02")))
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	exp, err := stdoutmetric.New(stdoutmetric.WithWriter(f))
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	shutdown := func(ctx context.Context) error {
		if err := exp.Shutdown(ctx); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	return exp, shutdown, nil
}

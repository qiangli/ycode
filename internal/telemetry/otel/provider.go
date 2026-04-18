package otel

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	otellog "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
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
	DataDir        string // root OTEL storage dir (e.g. ~/.agents/ycode/otel) — used for retention cleanup
	InstanceDir    string // per-instance subdir (e.g. ~/.agents/ycode/otel/instances/{id}) — used for file exports
	PersistTraces  bool
	PersistMetrics bool
	Opener         FileOpener // optional VFS-backed file opener for path validation
}

// Provider holds all three OTEL signal providers.
type Provider struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider
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

// grpcConnectTimeout limits how long we wait for gRPC exporter creation.
// If the collector is unreachable, we skip it rather than blocking startup.
const grpcConnectTimeout = 5 * time.Second

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
		grpcCtx, grpcCancel := context.WithTimeout(ctx, grpcConnectTimeout)
		grpcExp, err := otlptracegrpc.New(grpcCtx,
			otlptracegrpc.WithEndpoint(cfg.CollectorAddr),
			otlptracegrpc.WithInsecure(),
		)
		grpcCancel()
		if err != nil {
			slog.Warn("OTEL trace gRPC exporter unavailable, skipping", "addr", cfg.CollectorAddr, "error", err)
		} else {
			spanExporters = append(spanExporters, grpcExp)
			p.shutdownFuncs = append(p.shutdownFuncs, grpcExp.Shutdown)
		}
	}

	if cfg.PersistTraces && cfg.DataDir != "" {
		exportDir := cfg.InstanceDir
		if exportDir == "" {
			exportDir = cfg.DataDir
		}
		fileExp, shutdown, err := newRotatingTraceExporter(exportDir, cfg.Opener)
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
		grpcCtx, grpcCancel := context.WithTimeout(ctx, grpcConnectTimeout)
		grpcExp, err := otlpmetricgrpc.New(grpcCtx,
			otlpmetricgrpc.WithEndpoint(cfg.CollectorAddr),
			otlpmetricgrpc.WithInsecure(),
		)
		grpcCancel()
		if err != nil {
			slog.Warn("OTEL metric gRPC exporter unavailable, skipping", "addr", cfg.CollectorAddr, "error", err)
		} else {
			metricReaders = append(metricReaders,
				sdkmetric.NewPeriodicReader(grpcExp, sdkmetric.WithInterval(15*time.Second)))
			p.shutdownFuncs = append(p.shutdownFuncs, grpcExp.Shutdown)
		}
	}

	if cfg.PersistMetrics && cfg.DataDir != "" {
		exportDir := cfg.InstanceDir
		if exportDir == "" {
			exportDir = cfg.DataDir
		}
		fileExp, shutdown, err := newRotatingMetricExporter(exportDir, cfg.Opener)
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

	// --- Log provider (for structured log records to VictoriaLogs) ---
	if cfg.CollectorAddr != "" {
		grpcCtx, grpcCancel := context.WithTimeout(ctx, grpcConnectTimeout)
		grpcLogExp, err := otlploggrpc.New(grpcCtx,
			otlploggrpc.WithEndpoint(cfg.CollectorAddr),
			otlploggrpc.WithInsecure(),
		)
		grpcCancel()
		if err != nil {
			slog.Warn("OTEL log gRPC exporter unavailable, skipping", "addr", cfg.CollectorAddr, "error", err)
		} else {
			p.LoggerProvider = sdklog.NewLoggerProvider(
				sdklog.WithResource(res),
				sdklog.WithProcessor(sdklog.NewBatchProcessor(grpcLogExp)),
			)
			p.shutdownFuncs = append(p.shutdownFuncs, grpcLogExp.Shutdown)
			p.shutdownFuncs = append(p.shutdownFuncs, p.LoggerProvider.Shutdown)
			otellog.SetLoggerProvider(p.LoggerProvider)
		}
	}

	// Create pre-built instruments.
	inst, err := NewInstruments(p.MeterProvider.Meter(cfg.ServiceName))
	if err != nil {
		return nil, fmt.Errorf("create instruments: %w", err)
	}
	p.Instruments = inst

	return p, nil
}

// newRotatingTraceExporter creates a file-based trace exporter writing to dataDir/traces/.
func newRotatingTraceExporter(dataDir string, opener FileOpener) (sdktrace.SpanExporter, func(context.Context) error, error) {
	dir := filepath.Join(dataDir, "traces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	filename := filepath.Join(dir, fmt.Sprintf("traces-%s.jsonl", time.Now().Format("2006-01-02")))

	var f *os.File
	var err error
	if opener != nil {
		f, err = opener.OpenFile(context.Background(), filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	} else {
		f, err = os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	}
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
func newRotatingMetricExporter(dataDir string, opener FileOpener) (sdkmetric.Exporter, func(context.Context) error, error) {
	dir := filepath.Join(dataDir, "metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	filename := filepath.Join(dir, fmt.Sprintf("metrics-%s.jsonl", time.Now().Format("2006-01-02")))

	var f *os.File
	var err error
	if opener != nil {
		f, err = opener.OpenFile(context.Background(), filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	} else {
		f, err = os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	}
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

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
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
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
	PersistLogs    bool       // also write structured logs to disk (parity with traces/metrics)
	Opener         FileOpener // optional VFS-backed file opener for path validation
}

// Provider holds all three OTEL signal providers.
type Provider struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider
	Instruments    *Instruments
	resource       *resource.Resource // preserved for dynamic exporter addition
	metricReaders  []sdkmetric.Reader // preserved for MeterProvider rebuild
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

	p := &Provider{resource: res}

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

	p.metricReaders = metricReaders // preserve for MeterProvider rebuild in TryConnectCollector

	var meterOpts []sdkmetric.Option
	meterOpts = append(meterOpts, sdkmetric.WithResource(res))
	for _, r := range metricReaders {
		meterOpts = append(meterOpts, sdkmetric.WithReader(r))
	}
	p.MeterProvider = sdkmetric.NewMeterProvider(meterOpts...)
	p.shutdownFuncs = append(p.shutdownFuncs, p.MeterProvider.Shutdown)
	otel.SetMeterProvider(p.MeterProvider)

	// --- Log provider (structured records: gRPC to collector and/or rotating file) ---
	var logProcessors []sdklog.Processor

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
			logProcessors = append(logProcessors, sdklog.NewBatchProcessor(grpcLogExp))
			p.shutdownFuncs = append(p.shutdownFuncs, grpcLogExp.Shutdown)
		}
	}

	if cfg.PersistLogs && cfg.DataDir != "" {
		exportDir := cfg.InstanceDir
		if exportDir == "" {
			exportDir = cfg.DataDir
		}
		fileExp, shutdown, err := newRotatingLogExporter(exportDir, cfg.Opener)
		if err != nil {
			return nil, fmt.Errorf("create log file exporter: %w", err)
		}
		logProcessors = append(logProcessors, sdklog.NewBatchProcessor(fileExp))
		p.shutdownFuncs = append(p.shutdownFuncs, shutdown)
	}

	if len(logProcessors) > 0 {
		logOpts := []sdklog.LoggerProviderOption{sdklog.WithResource(res)}
		for _, proc := range logProcessors {
			logOpts = append(logOpts, sdklog.WithProcessor(proc))
		}
		p.LoggerProvider = sdklog.NewLoggerProvider(logOpts...)
		p.shutdownFuncs = append(p.shutdownFuncs, p.LoggerProvider.Shutdown)
		otellog.SetLoggerProvider(p.LoggerProvider)
	}

	// Create pre-built instruments.
	inst, err := NewInstruments(p.MeterProvider.Meter(cfg.ServiceName))
	if err != nil {
		return nil, fmt.Errorf("create instruments: %w", err)
	}
	p.Instruments = inst

	return p, nil
}

// TryConnectCollector attempts to add gRPC exporters to a running collector.
// If the collector is unreachable, it logs a warning and returns false.
// This enables graceful upgrade from file-only to dual-export mode.
func (p *Provider) TryConnectCollector(ctx context.Context, addr string) bool {
	if addr == "" {
		return false
	}

	// Try trace exporter.
	grpcCtx, grpcCancel := context.WithTimeout(ctx, grpcConnectTimeout)
	traceExp, err := otlptracegrpc.New(grpcCtx,
		otlptracegrpc.WithEndpoint(addr),
		otlptracegrpc.WithInsecure(),
	)
	grpcCancel()
	if err != nil {
		slog.Debug("otel: collector not available for traces", "addr", addr, "error", err)
		return false
	}

	// Try metric exporter.
	grpcCtx2, grpcCancel2 := context.WithTimeout(ctx, grpcConnectTimeout)
	metricExp, err := otlpmetricgrpc.New(grpcCtx2,
		otlpmetricgrpc.WithEndpoint(addr),
		otlpmetricgrpc.WithInsecure(),
	)
	grpcCancel2()
	if err != nil {
		slog.Debug("otel: collector not available for metrics", "addr", addr, "error", err)
		_ = traceExp.Shutdown(ctx)
		return false
	}

	// Try log exporter.
	grpcCtx3, grpcCancel3 := context.WithTimeout(ctx, grpcConnectTimeout)
	logExp, err := otlploggrpc.New(grpcCtx3,
		otlploggrpc.WithEndpoint(addr),
		otlploggrpc.WithInsecure(),
	)
	grpcCancel3()
	if err != nil {
		slog.Debug("otel: collector not available for logs", "addr", addr, "error", err)
		_ = traceExp.Shutdown(ctx)
		_ = metricExp.Shutdown(ctx)
		return false
	}

	// Register gRPC trace exporter as additional batch processor.
	p.TracerProvider.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(traceExp))
	p.shutdownFuncs = append(p.shutdownFuncs, traceExp.Shutdown)

	// Rebuild MeterProvider with the gRPC exporter added alongside existing file reader(s).
	// The SDK doesn't support dynamic reader addition, so we create a new provider.
	grpcReader := sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(15*time.Second))
	meterOpts := []sdkmetric.Option{sdkmetric.WithResource(p.resource)}
	for _, r := range p.metricReaders {
		meterOpts = append(meterOpts, sdkmetric.WithReader(r))
	}
	meterOpts = append(meterOpts, sdkmetric.WithReader(grpcReader))
	p.metricReaders = append(p.metricReaders, grpcReader)
	newMeter := sdkmetric.NewMeterProvider(meterOpts...)
	p.shutdownFuncs = append(p.shutdownFuncs, metricExp.Shutdown)
	p.shutdownFuncs = append(p.shutdownFuncs, newMeter.Shutdown)
	otel.SetMeterProvider(newMeter)

	// Rebuild instruments on the new meter so subsequent recordings go to
	// both exporters. Mutate the existing struct in place rather than
	// replacing p.Instruments — every consumer that captured the pointer
	// before TryConnectCollector ran (cmd/ycode/otel.go:96, :114, :216,
	// :230, internal/telemetry/otel/bridge.go) holds the same *Instruments
	// they always had, but its fields now point to counters bound to the
	// new MeterProvider (which has both file and gRPC readers). Without
	// this mutation, those captures stay bound to the file-only provider
	// and never reach Prometheus — that produced the systemic "No data"
	// panel issue across every ycode metric except those resolved per
	// call (e.g. the bus counters).
	if inst, err := NewInstruments(newMeter.Meter("ycode")); err == nil {
		*p.Instruments = *inst
	} else {
		slog.Warn("otel: instrument rebuild on collector connect failed", "error", err)
	}

	// Set up log provider with the original resource (preserves service.instance.id).
	if p.LoggerProvider == nil {
		p.LoggerProvider = sdklog.NewLoggerProvider(
			sdklog.WithResource(p.resource),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		)
		p.shutdownFuncs = append(p.shutdownFuncs, p.LoggerProvider.Shutdown)
		otellog.SetLoggerProvider(p.LoggerProvider)
	}
	p.shutdownFuncs = append(p.shutdownFuncs, logExp.Shutdown)

	slog.Info("otel: connected to collector", "addr", addr)
	return true
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

// newRotatingLogExporter creates a file-based log exporter writing to dataDir/logs/.
// File-mode parity with newRotatingTraceExporter / newRotatingMetricExporter:
// without this, structured logs are dropped whenever the gRPC collector is
// unreachable (the file-only mode common in standalone ycode runs).
func newRotatingLogExporter(dataDir string, opener FileOpener) (sdklog.Exporter, func(context.Context) error, error) {
	dir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	filename := filepath.Join(dir, fmt.Sprintf("logs-%s.jsonl", time.Now().Format("2006-01-02")))

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
	exp, err := stdoutlog.New(stdoutlog.WithWriter(f))
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

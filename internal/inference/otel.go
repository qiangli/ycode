package inference

import (
	"context"
	"log/slog"
	"sync/atomic"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// OTELConfig holds the OTEL instrumentation for the inference engine.
type OTELConfig struct {
	Tracer trace.Tracer
	Meter  metric.Meter
	Inst   *yotel.Instruments
}

// otelState holds runtime OTEL state for the inference component.
type otelState struct {
	cfg            *OTELConfig
	runnerHealthy  metric.Int64ObservableGauge
	runnerRestarts metric.Int64ObservableGauge
	runnerPort     metric.Int64ObservableGauge
	healthyVal     atomic.Int64
	restartsVal    atomic.Int64
	portVal        atomic.Int64
}

// SetOTEL configures OTEL instrumentation on the component.
// Must be called before Start().
func (o *OllamaComponent) SetOTEL(cfg *OTELConfig) {
	if cfg == nil {
		return
	}

	state := &otelState{cfg: cfg}

	meter := cfg.Meter
	if meter == nil {
		return
	}

	// Register observable gauges for runner state — these are polled at scrape time.
	state.runnerHealthy, _ = meter.Int64ObservableGauge(
		"ycode.inference.runner.healthy",
		metric.WithDescription("Whether the inference runner is healthy (1=yes, 0=no)"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(state.healthyVal.Load())
			return nil
		}),
	)

	state.runnerRestarts, _ = meter.Int64ObservableGauge(
		"ycode.inference.runner.restart_count",
		metric.WithDescription("Cumulative runner restart count"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(state.restartsVal.Load())
			return nil
		}),
	)

	state.runnerPort, _ = meter.Int64ObservableGauge(
		"ycode.inference.runner.port",
		metric.WithDescription("Port the inference runner is listening on"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(state.portVal.Load())
			return nil
		}),
	)

	o.otel = state
}

// updateOTELGauges syncs the component's runtime state into OTEL gauges.
// Called after runner state changes.
func (o *OllamaComponent) updateOTELGauges() {
	if o.otel == nil {
		return
	}
	if o.runner != nil {
		if o.runner.Healthy() {
			o.otel.healthyVal.Store(1)
		} else {
			o.otel.healthyVal.Store(0)
		}
		o.otel.restartsVal.Store(int64(o.runner.Restarts()))
		o.otel.portVal.Store(int64(o.runner.Port()))
	} else {
		o.otel.healthyVal.Store(0)
	}
}

// traceRunnerStart records a span for a runner start event.
func (o *OllamaComponent) traceRunnerStart(ctx context.Context) {
	if o.otel == nil || o.otel.cfg.Tracer == nil {
		return
	}

	_, span := o.otel.cfg.Tracer.Start(ctx, "ycode.inference.runner.start",
		trace.WithAttributes(
			yotel.AttrInferenceProvider.String("ollama"),
			yotel.AttrInferencePort.Int(o.runner.Port()),
		),
	)
	span.End()

	if o.otel.cfg.Inst != nil {
		o.otel.cfg.Inst.InferenceRunnerStarts.Add(ctx, 1)
	}
	slog.Info("otel: inference runner start recorded", "port", o.runner.Port())
}

// traceRunnerCrash records a span and metric for a runner crash event.
func (o *OllamaComponent) traceRunnerCrash(ctx context.Context, err error) {
	if o.otel == nil || o.otel.cfg.Tracer == nil {
		return
	}

	_, span := o.otel.cfg.Tracer.Start(ctx, "ycode.inference.runner.crash",
		trace.WithAttributes(
			yotel.AttrInferenceProvider.String("ollama"),
			yotel.AttrInferenceRestarts.Int(int(o.runner.Restarts())),
		),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()

	if o.otel.cfg.Inst != nil {
		o.otel.cfg.Inst.InferenceRunnerCrashes.Add(ctx, 1)
	}
}

// traceRunnerHealthCheck records a span for a health check cycle.
func (o *OllamaComponent) traceRunnerHealthCheck(ctx context.Context, healthy bool) {
	if o.otel == nil || o.otel.cfg.Tracer == nil {
		return
	}

	_, span := o.otel.cfg.Tracer.Start(ctx, "ycode.inference.runner.health_check",
		trace.WithAttributes(
			yotel.AttrInferenceProvider.String("ollama"),
			yotel.AttrInferenceHealthy.Bool(healthy),
			yotel.AttrInferencePort.Int(o.runner.Port()),
		),
	)
	span.End()
}

package inference

import (
	"context"
	"log/slog"
	"sync/atomic"

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
//
// Per-runner crash/restart counters were dropped along with the
// external-binary RunnerManager — ollama's in-process scheduler owns
// runner lifecycle and doesn't expose a stable per-runner identity
// (runners are ephemeral, one per model load). The two gauges that
// remain (overall component health, API port) are observable from
// the outside.
type otelState struct {
	cfg           *OTELConfig
	runnerHealthy metric.Int64ObservableGauge
	runnerPort    metric.Int64ObservableGauge
	healthyVal    atomic.Int64
	portVal       atomic.Int64
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
// Runner-specific gauges (restarts, runner port) are no longer
// meaningful — ollama's scheduler owns runner lifecycle internally and
// uses ephemeral ports per request. We export the component's overall
// health and the ollama API port (default 11434).
func (o *OllamaComponent) updateOTELGauges() {
	if o.otel == nil {
		return
	}
	if o.healthy.Load() {
		o.otel.healthyVal.Store(1)
	} else {
		o.otel.healthyVal.Store(0)
	}
	o.otel.portVal.Store(int64(o.Port()))
}

// traceRunnerStart records a span for component start. Kept under the
// "runner.start" name for dashboard backward-compat; the actual runner
// subprocess is spawned later by ollama's scheduler on first inference.
func (o *OllamaComponent) traceRunnerStart(ctx context.Context) {
	if o.otel == nil || o.otel.cfg.Tracer == nil {
		return
	}

	_, span := o.otel.cfg.Tracer.Start(ctx, "ycode.inference.runner.start",
		trace.WithAttributes(
			yotel.AttrInferenceProvider.String("ollama"),
			yotel.AttrInferencePort.Int(o.Port()),
		),
	)
	span.End()

	if o.otel.cfg.Inst != nil {
		o.otel.cfg.Inst.InferenceRunnerStarts.Add(ctx, 1)
	}
	slog.Info("otel: inference component start recorded", "port", o.Port())
}

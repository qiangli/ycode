package inference

import (
	"context"
	"log/slog"
	"sync/atomic"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/coreutils/external/ollama"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// OTELConfig holds the OTEL instrumentation for the inference engine.
type OTELConfig struct {
	Tracer trace.Tracer
	Meter  metric.Meter
	Inst   *yotel.Instruments
}

// WireTelemetry configures OTEL instrumentation on the OllamaComponent via hooks.
func WireTelemetry(comp *ollama.OllamaComponent, cfg *OTELConfig) {
	if cfg == nil || comp == nil {
		return
	}

	meter := cfg.Meter
	if meter == nil {
		return
	}

	var healthyVal atomic.Int64
	var portVal atomic.Int64

	// Register observable gauges for runner state — these are polled at scrape time.
	_, _ = meter.Int64ObservableGauge(
		"ycode.inference.runner.healthy",
		metric.WithDescription("Whether the inference runner is healthy (1=yes, 0=no)"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(healthyVal.Load())
			return nil
		}),
	)

	_, _ = meter.Int64ObservableGauge(
		"ycode.inference.runner.port",
		metric.WithDescription("Port the inference runner is listening on"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(portVal.Load())
			return nil
		}),
	)

	// Set the callbacks on the component.
	comp.Hooks = ollama.TelemetryHooks{
		OnRunnerStart: func(ctx context.Context, port int) {
			if cfg.Tracer != nil {
				_, span := cfg.Tracer.Start(ctx, "ycode.inference.runner.start",
					trace.WithAttributes(
						yotel.AttrInferenceProvider.String("ollama"),
						yotel.AttrInferencePort.Int(port),
					),
				)
				span.End()
			}
			if cfg.Inst != nil {
				cfg.Inst.InferenceRunnerStarts.Add(ctx, 1)
			}
			slog.Info("otel: inference component start recorded", "port", port)
		},
		OnStatusChange: func(healthy bool, port int) {
			if healthy {
				healthyVal.Store(1)
			} else {
				healthyVal.Store(0)
			}
			portVal.Store(int64(port))
		},
	}
}

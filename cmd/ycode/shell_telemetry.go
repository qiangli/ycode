package main

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/shell"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// wireShellTelemetry binds agentmode's MetricsSink and TracerSink to the
// real OTel Instruments and a shell tracer. Called from setupFileOTEL /
// setupOTEL once otelProvider is up. Safe to call repeatedly — a later
// call replaces an earlier sink.
//
// If otelProvider has nil Instruments (init partially failed), this is
// a no-op for metrics; the tracer still gets installed because the OTel
// SDK provides a no-op meter in that case.
func wireShellTelemetry(p *yotel.Provider) {
	if p == nil {
		return
	}
	tracer := p.Tracer("ycode.shell")
	shell.SetTracer(otelTracerSink{tracer: tracer})
	if p.Instruments != nil {
		shell.SetMetrics(otelMetricsSink{inst: p.Instruments})
	}
}

// otelMetricsSink implements agentmode.MetricsSink over real OTel
// counters from the shared Instruments struct.
type otelMetricsSink struct {
	inst *yotel.Instruments
}

func (s otelMetricsSink) ObserveHint(id, category, phase string) {
	if s.inst == nil || s.inst.ShellHintsFiredTotal == nil {
		return
	}
	s.inst.ShellHintsFiredTotal.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("hint_id", id),
		attribute.String("category", category),
		attribute.String("phase", phase),
	))
}

func (s otelMetricsSink) ObserveMineWrite(phase, outcome string) {
	if s.inst == nil || s.inst.ShellMineRecordTotal == nil {
		return
	}
	s.inst.ShellMineRecordTotal.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("phase", phase),
		attribute.String("outcome", outcome),
	))
}

func (s otelMetricsSink) ObserveIntent(kind string) {
	if s.inst == nil || s.inst.ShellIntentClassifiedTotal == nil {
		return
	}
	s.inst.ShellIntentClassifiedTotal.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("kind", kind),
	))
}

func (s otelMetricsSink) ObserveCommandDuration(intentKind string, durationMs float64) {
	if s.inst == nil || s.inst.ShellCommandDuration == nil {
		return
	}
	s.inst.ShellCommandDuration.Record(context.Background(), durationMs, metric.WithAttributes(
		attribute.String("intent_kind", intentKind),
	))
}

// otelTracerSink implements agentmode.TracerSink over a real OTel tracer.
type otelTracerSink struct {
	tracer trace.Tracer
}

func (t otelTracerSink) StartSpan(ctx context.Context, name string) (context.Context, shell.SpanEnd) {
	ctx, span := t.tracer.Start(ctx, name)
	return ctx, func(err error, attrs ...string) {
		// attrs is a flat list of (k, v, k, v, ...) string pairs.
		for i := 0; i+1 < len(attrs); i += 2 {
			span.SetAttributes(attribute.String(attrs[i], attrs[i+1]))
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

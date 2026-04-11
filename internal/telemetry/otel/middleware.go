package otel

import (
	"context"
	"encoding/json"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ToolFunc matches the tools.ToolFunc signature.
type ToolFunc func(ctx context.Context, input json.RawMessage) (string, error)

// ToolMiddleware returns a function that wraps a tool handler with OTEL
// tracing and metrics, capturing full input/output for self-healing and audit.
func ToolMiddleware(tracer trace.Tracer, inst *Instruments) func(toolName string, next ToolFunc) ToolFunc {
	return func(toolName string, next ToolFunc) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			ctx, span := tracer.Start(ctx, "ycode.tool.call",
				trace.WithAttributes(
					AttrToolName.String(toolName),
					AttrToolInputSummary.String(truncate(string(input), 512)),
				))
			defer span.End()

			// Record full input as span event.
			span.AddEvent("tool.input_received", trace.WithAttributes(
				attribute.String("input", string(input)),
			))

			start := time.Now()
			output, err := next(ctx, input)
			dur := time.Since(start)

			span.SetAttributes(
				AttrToolDurationMs.Int64(dur.Milliseconds()),
				AttrToolOutputSize.Int(len(output)),
				AttrToolOutputSummary.String(truncate(output, 512)),
				AttrToolSuccess.Bool(err == nil),
			)

			// Record full output as span event.
			span.AddEvent("tool.output_produced", trace.WithAttributes(
				attribute.String("output", output),
			))

			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				span.AddEvent("tool.error_occurred", trace.WithAttributes(
					attribute.String("error", err.Error()),
				))
			}

			// Record metrics.
			inst.ToolCallDuration.Record(ctx, float64(dur.Milliseconds()),
				metric.WithAttributes(AttrToolName.String(toolName)))
			inst.ToolCallTotal.Add(ctx, 1,
				metric.WithAttributes(
					AttrToolName.String(toolName),
					AttrToolSuccess.Bool(err == nil),
				))

			return output, err
		}
	}
}

// truncate returns at most maxLen characters of s.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

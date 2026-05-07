package otel

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Maximum stack-trace bytes captured on a span event. Full trace still
// goes to slog so we don't lose detail; the span attribute caps to keep
// trace storage cheap.
const panicStackSpanLimit = 4096

var (
	panicCounterOnce sync.Once
	panicCounter     metric.Int64Counter
)

// ensurePanicCounter lazily creates the runtime panic counter the first
// time RecordPanic is called. We can't bind it during NewProvider
// because the panic safety net runs in packages that don't take an
// *Instruments handle (tools/registry, taskqueue, spawner), and a
// cross-package panic helper has to work even before NewProvider has
// run (e.g. during early bootstrap).
func ensurePanicCounter() {
	panicCounterOnce.Do(func() {
		c, err := otel.Meter("ycode.runtime").Int64Counter(
			"ycode.runtime.panic.total",
			metric.WithDescription("Panics caught by the runtime safety net (tools, taskqueue, spawner)"),
		)
		if err == nil {
			panicCounter = c
		}
	})
}

// RecordPanic captures a recovered panic into OTel. Pass the recovered
// value (the result of a `recover()` call), a short component label
// like "tools.invoke", and an optional detail (typically the tool or
// task name). Returns an error describing the panic, or nil if the
// recovered value is nil — so callers can write:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        err = otel.RecordPanic(ctx, "tools.invoke", name, r)
//	    }
//	}()
//
// On a real panic this records:
//   - `ycode.runtime.panic.total` counter with component+detail attrs
//   - a span event with the recovered value and a truncated stack
//   - a structured slog error with the full stack
func RecordPanic(ctx context.Context, component, detail string, recovered any) error {
	if recovered == nil {
		return nil
	}
	ensurePanicCounter()
	stack := debug.Stack()

	if panicCounter != nil {
		panicCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("component", component),
				attribute.String("detail", detail),
			),
		)
	}

	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		stackForSpan := stack
		if len(stackForSpan) > panicStackSpanLimit {
			stackForSpan = stackForSpan[:panicStackSpanLimit]
		}
		span.AddEvent("panic", trace.WithAttributes(
			attribute.String("component", component),
			attribute.String("detail", detail),
			attribute.String("recovered", fmt.Sprint(recovered)),
			attribute.String("stack", string(stackForSpan)),
		))
		span.RecordError(fmt.Errorf("panic: %v", recovered))
	}

	slog.Error("panic recovered",
		"component", component,
		"detail", detail,
		"recovered", fmt.Sprint(recovered),
		"stack", string(stack),
	)

	return fmt.Errorf("%s panicked (%s): %v", component, detail, recovered)
}

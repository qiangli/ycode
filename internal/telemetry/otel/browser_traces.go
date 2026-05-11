package otel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const browserTracerName = "ycode.browser"

// Browser-flavored span attribute keys. URL + selector live ONLY on
// spans (high cardinality) — never metric labels.
var (
	AttrBrowserMode    = attribute.Key("browser.mode")
	AttrBrowserAction  = attribute.Key("browser.action")
	AttrBrowserAcURL   = attribute.Key("browser.action.url")
	AttrBrowserAcSel   = attribute.Key("browser.action.selector")
	AttrBrowserOutcome = attribute.Key("browser.outcome")
	AttrBrowserHints   = attribute.Key("browser.hints")
	AttrBrowserDurMs   = attribute.Key("browser.duration_ms")
)

// StartBrowserActionSpan begins a span around one BrowserAction
// dispatch. The caller must invoke the returned `finish` once, passing
// outcome + hints + error so the span is properly tagged and ended.
//
//	ctx, finish := StartBrowserActionSpan(ctx, "live", "navigate", url, selector)
//	defer finish(outcome, hints, err)
//
// The finish closure also emits the action metrics so callers don't
// double-instrument.
func StartBrowserActionSpan(ctx context.Context, mode, action, url, selector string) (context.Context, func(outcome string, hints []string, err error)) {
	tracer := otel.Tracer(browserTracerName)
	attrs := []attribute.KeyValue{
		AttrBrowserMode.String(mode),
		AttrBrowserAction.String(action),
	}
	if url != "" {
		attrs = append(attrs, AttrBrowserAcURL.String(url))
	}
	if selector != "" {
		attrs = append(attrs, AttrBrowserAcSel.String(selector))
	}
	ctx, span := tracer.Start(ctx, "ycode.browser."+action, trace.WithAttributes(attrs...))
	start := time.Now()

	return ctx, func(outcome string, hints []string, err error) {
		dur := time.Since(start)
		if outcome != "" {
			span.SetAttributes(AttrBrowserOutcome.String(outcome))
		}
		if len(hints) > 0 {
			span.SetAttributes(AttrBrowserHints.StringSlice(hints))
		}
		span.SetAttributes(AttrBrowserDurMs.Int64(dur.Milliseconds()))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if outcome == "BLOCKED" || outcome == "WRONG_ELEMENT" {
			span.SetStatus(codes.Error, "outcome="+outcome)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
		RecordBrowserAction(ctx, mode, action, outcome, dur)
	}
}

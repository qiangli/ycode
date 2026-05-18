package otel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Browser telemetry covers the three pure-Go modes (live, probe,
// solo) and the reliability layer that wraps them. Attributes are
// kept coarse to bound cardinality:
//
//   - mode:   "live" | "probe" | "solo"
//   - action: action type (navigate, click, …)
//   - outcome: outcome class (SUCCESS | SILENT_CLICK | …)
//
// URL, selector text, page content, etc. are NEVER carried as metric
// attributes — they live in span attributes (Pulse traces) instead.
//
// Instruments are looked up per call (not memoized) because some
// browser code paths fire before the global MeterProvider is wired
// (e.g. the live hub binds during runAllServices, well before
// setupOTEL replaces the no-op provider). Cost of per-call lookup is
// negligible — OTEL's meter caches counter creation internally.

func browserMeter() metric.Meter { return otel.Meter("ycode.browser") }

// RecordBrowserAction reports one Execute round-trip. mode is "live"
// | "probe" | "solo"; action is the BrowserAction.Type; outcome is the
// reliability layer's OutcomeClass (or "ERROR" when Execute returned a
// non-nil error before classification). Empty outcome → "UNKNOWN".
func RecordBrowserAction(ctx context.Context, mode, action, outcome string, dur time.Duration) {
	if outcome == "" {
		outcome = "UNKNOWN"
	}
	m := browserMeter()
	counter, err := m.Int64Counter(
		"ycode.browser.action.total",
		metric.WithDescription("Browser actions invoked, tagged by mode/action/outcome"),
	)
	if err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("mode", mode),
			attribute.String("action", action),
			attribute.String("outcome", outcome),
		))
	}
	hist, err := m.Float64Histogram(
		"ycode.browser.action.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("End-to-end browser action latency through the reliability layer"),
	)
	if err == nil {
		hist.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(
			attribute.String("mode", mode),
			attribute.String("action", action),
			attribute.String("outcome", outcome),
		))
	}
}

// RecordBrowserHint fires once per Hint Engine rule match.
func RecordBrowserHint(ctx context.Context, mode, rule string) {
	counter, err := browserMeter().Int64Counter(
		"ycode.browser.hint.fired.total",
		metric.WithDescription("Hint Engine rule firings, tagged by rule name"),
	)
	if err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("mode", mode),
			attribute.String("rule", rule),
		))
	}
}

// RecordBrowserBreakerTrip is called when the circuit breaker
// short-circuits an action. level is "element" | "page" | "global".
func RecordBrowserBreakerTrip(ctx context.Context, mode, level string) {
	counter, err := browserMeter().Int64Counter(
		"ycode.browser.breaker.trips.total",
		metric.WithDescription("Circuit-breaker short-circuits, tagged by level (element/page/global)"),
	)
	if err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("mode", mode),
			attribute.String("level", level),
		))
	}
}

// RecordBrowserRalphExhausted fires once when every Ralph strategy
// has failed. attemptCount is the number of strategies tried (1..N).
// Useful for monitoring whether ralph is doing its job (the ratio
// of exhausted runs vs. RecordBrowserRalphAttempt(_, _, _, true) =
// how often the fallback layer is meaningfully changing outcomes).
func RecordBrowserRalphExhausted(ctx context.Context, mode string, attemptCount int) {
	counter, err := browserMeter().Int64Counter(
		"ycode.browser.ralph.exhausted.total",
		metric.WithDescription("Ralph fallback runs where every strategy failed"),
	)
	if err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("mode", mode),
			attribute.Int("attempts", attemptCount),
		))
	}
}

// RecordBrowserRalphAttempt records one strategy attempt in the Ralph
// click fallback. strategy: "as-given" | "trimmed" | "unquoted" |
// "js-click" | "js-text-click" | "extract-click-by-text".
func RecordBrowserRalphAttempt(ctx context.Context, mode, strategy string, succeeded bool) {
	counter, err := browserMeter().Int64Counter(
		"ycode.browser.ralph.attempts.total",
		metric.WithDescription("Ralph fallback strategy attempts, tagged by strategy and success"),
	)
	if err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("mode", mode),
			attribute.String("strategy", strategy),
			attribute.Bool("succeeded", succeeded),
		))
	}
}

// RecordBrowserCompactRatio reports compression ratio for one
// DOM-compression pass. ratio is bytesOut/bytesIn (1.0 = no
// compression; smaller = better).
func RecordBrowserCompactRatio(ctx context.Context, mode string, bytesIn, bytesOut int) {
	if bytesIn <= 0 {
		return
	}
	hist, err := browserMeter().Float64Histogram(
		"ycode.browser.dom.compression_ratio",
		metric.WithUnit("1"),
		metric.WithDescription("Output bytes / input bytes for the DOM-compression pass"),
	)
	if err == nil {
		hist.Record(ctx, float64(bytesOut)/float64(bytesIn), metric.WithAttributes(
			attribute.String("mode", mode),
		))
	}
}

// RecordBrowserHubConnect / Disconnect maintain the live-mode
// extension connection gauge AND increment the lifetime connects
// counter so disconnect rate is calculable.
func RecordBrowserHubConnect(ctx context.Context) {
	gauge, err := browserMeter().Int64UpDownCounter(
		"ycode.browser.live.connections",
		metric.WithDescription("Currently-attached live-mode Chrome extension WebSockets"),
	)
	if err == nil {
		gauge.Add(ctx, 1)
	}
	counter, err := browserMeter().Int64Counter(
		"ycode.browser.live.connects.total",
		metric.WithDescription("Lifetime count of extension WebSocket attaches"),
	)
	if err == nil {
		counter.Add(ctx, 1)
	}
}

func RecordBrowserHubDisconnect(ctx context.Context) {
	gauge, err := browserMeter().Int64UpDownCounter(
		"ycode.browser.live.connections",
		metric.WithDescription("Currently-attached live-mode Chrome extension WebSockets"),
	)
	if err == nil {
		gauge.Add(ctx, -1)
	}
}

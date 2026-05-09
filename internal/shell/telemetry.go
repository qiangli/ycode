package shell

import (
	"context"
	"sync"
)

// MetricsSink lets the shell layer emit OTel-shaped counters without
// importing OTel directly. Wire an implementation via SetMetrics from
// cmd/ycode (which can pull in telemetry/otel and bind these calls to
// real Instruments). Mirrors the SetSuggestFunc / SetPostHintsFunc
// pattern already used in this package.
type MetricsSink interface {
	// ObserveHint records that a hint with the given id and category
	// fired in the given phase ("pre" or "post").
	ObserveHint(id, category, phase string)
	// ObserveMineWrite records the outcome of an attempted JSONL sink
	// write — "ok", "disabled", "open_err", "encode_err", "mkdir_err",
	// "no_path".
	ObserveMineWrite(phase, outcome string)
	// ObserveIntent records a sentinel.Classify result, labeled by the
	// classified kind (bash, slash, skill, ...).
	ObserveIntent(kind string)
	// ObserveCommandDuration records the wall-clock latency of a single
	// shell command's full Classify→Dispatch arc.
	ObserveCommandDuration(intentKind string, durationMs float64)
}

// TracerSink starts an OTel span without importing OTel. The returned
// SpanEnd is called with the eventual error (or nil) and any extra
// attribute pairs the caller wants to attach at end-time.
type TracerSink interface {
	StartSpan(ctx context.Context, name string) (context.Context, SpanEnd)
}

// SpanEnd terminates a span. attrs is a flat list of key/value string
// pairs; non-string types are out of scope for the shell surface.
type SpanEnd func(err error, attrs ...string)

var (
	telemetryMu sync.RWMutex
	metricsSink MetricsSink
	tracerSink  TracerSink
)

// SetMetrics installs the metrics sink. Pass nil to clear (used by
// tests). Safe for concurrent use.
func SetMetrics(s MetricsSink) {
	telemetryMu.Lock()
	defer telemetryMu.Unlock()
	metricsSink = s
}

// SetTracer installs the tracer sink. Pass nil to clear.
func SetTracer(s TracerSink) {
	telemetryMu.Lock()
	defer telemetryMu.Unlock()
	tracerSink = s
}

// ObserveHint forwards to the registered MetricsSink, no-op if unset.
// Exported so the agentmode package (which lives below shell in the
// import graph) can call it.
func ObserveHint(id, category, phase string) {
	telemetryMu.RLock()
	s := metricsSink
	telemetryMu.RUnlock()
	if s != nil {
		s.ObserveHint(id, category, phase)
	}
}

// ObserveMineWrite forwards to the registered MetricsSink, no-op if unset.
func ObserveMineWrite(phase, outcome string) {
	telemetryMu.RLock()
	s := metricsSink
	telemetryMu.RUnlock()
	if s != nil {
		s.ObserveMineWrite(phase, outcome)
	}
}

// observeIntent is package-local because only sentinel.Classify uses it.
func observeIntent(kind string) {
	telemetryMu.RLock()
	s := metricsSink
	telemetryMu.RUnlock()
	if s != nil {
		s.ObserveIntent(kind)
	}
}

// ObserveCommandDuration forwards to the registered MetricsSink. Exposed
// so the cmd-level dispatcher (cmd/ycode/shell_cmd.go) can record total
// latency without owning the sink directly.
func ObserveCommandDuration(intentKind string, durationMs float64) {
	telemetryMu.RLock()
	s := metricsSink
	telemetryMu.RUnlock()
	if s != nil {
		s.ObserveCommandDuration(intentKind, durationMs)
	}
}

// StartSpan forwards to the registered TracerSink, returning a no-op
// SpanEnd if unset so callers can defer it unconditionally.
func StartSpan(ctx context.Context, name string) (context.Context, SpanEnd) {
	telemetryMu.RLock()
	s := tracerSink
	telemetryMu.RUnlock()
	if s == nil {
		return ctx, func(error, ...string) {}
	}
	return s.StartSpan(ctx, name)
}

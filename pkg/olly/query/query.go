// Package query provides a typed in-process Go API over ycode's embedded
// observability backends (Prometheus metrics, Jaeger traces, VictoriaLogs
// logs). It lets ycode components introspect their own past behavior — the
// programmatic counterpart to the human-facing dashboards — without going
// through HTTP indirection or learning each backend's native query language.
//
// All filters and results use OTel semantic-convention attribute names
// (service.name, session.id, trace.id, etc.) so the same filter shape
// works across signals.
package query

import (
	"context"
	"errors"
	"time"
)

// Querier is the unified read API over metrics, traces, and logs.
//
// Implementations may return ErrUnsupported for any signal type whose
// backend is not configured.
type Querier interface {
	// QueryPromQL evaluates a PromQL expression at a single instant.
	QueryPromQL(ctx context.Context, expr string, at time.Time) (Result, error)

	// QueryPromQLRange evaluates a PromQL expression over a time range.
	QueryPromQLRange(ctx context.Context, expr string, start, end time.Time, step time.Duration) (Result, error)

	// Traces returns traces matching the filter.
	Traces(ctx context.Context, f TraceFilter) ([]Trace, error)

	// Logs returns log records matching the filter.
	Logs(ctx context.Context, f LogFilter) ([]LogRecord, error)
}

// ErrUnsupported is returned by a Querier when the backend for the
// requested signal type is not configured.
var ErrUnsupported = errors.New("olly/query: backend not configured")

// TraceFilter selects traces by service, trace ID, session ID, or time range.
// Empty fields are wildcards. SessionID is a shortcut for the OTel attribute
// session.id; ycode emits this on every span so it is the primary key for
// "what happened during user query X".
type TraceFilter struct {
	Service   string    // matches resource attribute service.name
	TraceID   string    // exact match; if set, other filters are ignored
	SessionID string    // matches span attribute session.id
	Operation string    // matches span name (operation)
	Start     time.Time // inclusive; zero means unbounded
	End       time.Time // inclusive; zero means now
	Limit     int       // 0 → backend default
}

// LogFilter selects log records by service, trace ID, session ID, level, or
// a free-text query. The Query field accepts the underlying backend's
// native query syntax (LogsQL for VictoriaLogs); leave empty for none.
type LogFilter struct {
	Service   string
	TraceID   string
	SessionID string
	Level     string // OTel severity text: "ERROR", "WARN", "INFO", "DEBUG", "TRACE"
	Query     string
	Start     time.Time
	End       time.Time
	Limit     int
}

// Trace is a single distributed trace — a tree of spans sharing a trace ID.
type Trace struct {
	TraceID  string
	Service  string // dominant service.name on the trace's root span
	Start    time.Time
	Duration time.Duration
	Spans    []Span
}

// Span is one span within a trace.
type Span struct {
	SpanID   string
	ParentID string
	Name     string
	Service  string
	Start    time.Time
	Duration time.Duration
	Status   string            // "OK", "ERROR", "UNSET"
	Attrs    map[string]string // span and resource attributes flattened
}

// LogRecord is a single log entry.
type LogRecord struct {
	Time    time.Time
	Service string
	TraceID string
	SpanID  string
	Level   string
	Body    string
	Attrs   map[string]string
}

// Result is the outcome of a PromQL evaluation. It mirrors the shape of
// the Prometheus HTTP API: a result type ("vector", "matrix", "scalar",
// "string") plus a list of labeled time series.
type Result struct {
	Type   string
	Series []Series
}

// Series is a labeled stream of samples. For instant queries a Series
// usually has exactly one Sample; for range queries it has many.
type Series struct {
	Labels  map[string]string
	Samples []Sample
}

// Sample is a single (time, value) data point.
type Sample struct {
	Time  time.Time
	Value float64
}

// Backends bundles per-signal query backends. Any nil field means that
// signal type is unsupported on this Querier.
type Backends struct {
	Metrics MetricsBackend
	Traces  TracesBackend
	Logs    LogsBackend
}

// MetricsBackend evaluates PromQL queries.
type MetricsBackend interface {
	Instant(ctx context.Context, expr string, at time.Time) (Result, error)
	Range(ctx context.Context, expr string, start, end time.Time, step time.Duration) (Result, error)
}

// TracesBackend retrieves traces.
type TracesBackend interface {
	Traces(ctx context.Context, f TraceFilter) ([]Trace, error)
}

// LogsBackend retrieves log records.
type LogsBackend interface {
	Logs(ctx context.Context, f LogFilter) ([]LogRecord, error)
}

// New returns a Querier that dispatches to the supplied backends.
func New(b Backends) Querier {
	return &composite{b: b}
}

type composite struct {
	b Backends
}

func (c *composite) QueryPromQL(ctx context.Context, expr string, at time.Time) (Result, error) {
	if c.b.Metrics == nil {
		return Result{}, ErrUnsupported
	}
	if at.IsZero() {
		at = time.Now()
	}
	return c.b.Metrics.Instant(ctx, expr, at)
}

func (c *composite) QueryPromQLRange(ctx context.Context, expr string, start, end time.Time, step time.Duration) (Result, error) {
	if c.b.Metrics == nil {
		return Result{}, ErrUnsupported
	}
	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-1 * time.Hour)
	}
	if step <= 0 {
		step = 15 * time.Second
	}
	return c.b.Metrics.Range(ctx, expr, start, end, step)
}

func (c *composite) Traces(ctx context.Context, f TraceFilter) ([]Trace, error) {
	if c.b.Traces == nil {
		return nil, ErrUnsupported
	}
	return c.b.Traces.Traces(ctx, f)
}

func (c *composite) Logs(ctx context.Context, f LogFilter) ([]LogRecord, error) {
	if c.b.Logs == nil {
		return nil, ErrUnsupported
	}
	return c.b.Logs.Logs(ctx, f)
}

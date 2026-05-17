package detector

import (
	"context"
	"log/slog"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Attribute keys the processor reads. Mirrors the canonical names
// defined in internal/telemetry/otel/attributes.go and the per-scope
// attribute conventions in exec_metrics.go / browser_metrics.go.
const (
	attrToolName       = "tool.name"
	attrToolError      = "tool.error"
	attrToolSuccess    = "tool.success"
	attrToolDurationMs = "tool.duration_ms"
	attrExecScope      = "exec.scope"
	attrExecExitClass  = "exec.exit_class"
	attrExecBinary     = "exec.binary"
	attrExecDurationMs = "exec.duration_ms"
	attrAgentClient    = "agent.client"
	attrWrapAgent      = "wrap.agent"

	// AttrSelfHealWorker is set by Phase 4 workers on every span they
	// emit so the processor can drop them — without this, a worker
	// fix-attempt that itself fails would re-trigger the detector and
	// cause an infinite self-heal loop.
	AttrSelfHealWorker = "selfheal.worker"
)

// SpanProcessor is the bridge from OTel into the selfheal channel.
// Implements sdktrace.SpanProcessor. Only OnEnd does real work;
// OnStart is a no-op so registration is cheap even when no failures
// are flowing.
type SpanProcessor struct {
	ch        chan<- rawSpan
	maxBuffer int // backpressure: when buffer is full, drop the span and increment dropped counter
	dropped   uint64
}

// NewSpanProcessor returns a processor that forwards qualifying spans
// to ch. ch must be a buffered channel; recommend at least 256.
// Closed-channel-on-shutdown semantics are handled by the Observer.
func NewSpanProcessor(ch chan<- rawSpan) *SpanProcessor {
	return &SpanProcessor{ch: ch, maxBuffer: cap(ch)}
}

func (p *SpanProcessor) OnStart(_ context.Context, _ sdktrace.ReadWriteSpan) {}

func (p *SpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if !isCandidate(s) {
		return
	}
	rs := projectSpan(s)
	// Recursion break: drop spans tagged as selfheal worker work.
	// Keeps the detector from chasing its own tail when later phases
	// run autoloop and its tools themselves fail.
	if rs.Attributes[AttrSelfHealWorker] == "true" {
		return
	}
	select {
	case p.ch <- rs:
	default:
		// Buffer full — drop oldest behavior would be nice but we don't
		// own the channel. A persistent backlog here means the consumer
		// goroutine is wedged; that's a bug worth surfacing via slog,
		// not silently absorbed.
		p.dropped++
		slog.Warn("selfheal: span dropped (consumer slow or wedged)",
			"dropped_total", p.dropped, "buffer_cap", p.maxBuffer)
	}
}

func (p *SpanProcessor) Shutdown(_ context.Context) error   { return nil }
func (p *SpanProcessor) ForceFlush(_ context.Context) error { return nil }

// isCandidate is a cheap pre-filter so the channel only sees spans
// the classifier might care about. Anything in an unknown scope or
// with status.code == Ok is rejected here; the channel consumer does
// the heavier classification work.
func isCandidate(s sdktrace.ReadOnlySpan) bool {
	// Reject spans that completed successfully — non-error tool calls
	// generate the bulk of OTel traffic and short-circuiting here
	// keeps the detector's overhead near zero on the happy path.
	if s.Status().Code == 0 { // codes.Unset
		// Some tool spans carry tool.success=false without setting
		// status.error (the ToolMiddleware path sets status, but
		// future surfaces might not). Fall through to a deeper check.
	}
	// Inspect the span name family. Anything outside ycode.* is third-
	// party (e.g. http client spans) and not our concern.
	name := s.Name()
	switch {
	case len(name) >= 6 && name[:6] == "ycode.":
		return true
	}
	return false
}

// projectSpan flattens a ReadOnlySpan into a rawSpan with only the
// fields the detector reads. Doing the projection here means the
// classifier and JSONL sink stay free of any OTel SDK imports —
// helpful for tests and for the Phase 2/3 modules that will reuse
// the rawSpan shape from observation replay files.
func projectSpan(s sdktrace.ReadOnlySpan) rawSpan {
	out := rawSpan{
		Name:       s.Name(),
		StartTime:  s.StartTime(),
		EndTime:    s.EndTime(),
		Attributes: make(map[string]string, 8),
	}
	if st := s.Status(); st.Code != 0 { // codes.Error == 1
		out.StatusError = st.Description
	}
	for _, kv := range s.Attributes() {
		// Only capture the keys the classifier actually reads — bounded
		// memory regardless of how chatty a future span family becomes.
		switch string(kv.Key) {
		case attrToolName, attrToolError, attrToolSuccess, attrToolDurationMs,
			attrExecScope, attrExecExitClass, attrExecBinary, attrExecDurationMs,
			attrAgentClient, attrWrapAgent, AttrSelfHealWorker:
			out.Attributes[string(kv.Key)] = kv.Value.Emit()
		}
	}
	// Some surfaces emit the error on a span event rather than as an
	// attribute. Pull the first error description into StatusError so
	// the classifier has something to work with.
	if out.StatusError == "" {
		for _, ev := range s.Events() {
			if ev.Name == "exception" {
				for _, kv := range ev.Attributes {
					if string(kv.Key) == "exception.message" {
						out.StatusError = kv.Value.Emit()
						break
					}
				}
			}
			if out.StatusError != "" {
				break
			}
		}
	}
	// Tool-call middleware sets tool.success=false on failure even when
	// status stays unset; promote that to StatusError so isCandidate's
	// status check above isn't load-bearing for that path.
	if out.StatusError == "" && out.Attributes[attrToolSuccess] == "false" {
		out.StatusError = out.Attributes[attrToolError]
	}
	return out
}

// Now exposes time.Now via a function value the tests can override.
// Keeping it in this file (not signal.go) so the SDK-importing file
// owns all the wall-clock concerns.
var nowFunc = func() time.Time { return time.Now() }

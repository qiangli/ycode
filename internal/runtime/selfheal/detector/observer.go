package detector

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Observer is the top-level coordinator for the Phase 1 selfheal
// pipeline: SpanProcessor → channel → classifier → dedupe → Sink.
//
// Lifecycle:
//
//	obs := NewObserver(Config{SinkPath: "..."})
//	obs.Start(ctx)
//	// register obs.Processor() on the global TracerProvider
//	// ... ycode serve runs ...
//	obs.Stop(ctx) // drains the channel, closes the sink
//
// One Observer per ycode serve process. Channel-driven so the
// SpanProcessor's OnEnd never blocks the OTel BSP behind classifier
// work or disk I/O.
type Observer struct {
	cfg        Config
	processor  *SpanProcessor
	classifier *Classifier
	dedupe     *Dedupe
	sink       Sink

	ch     chan rawSpan
	stopCh chan struct{}
	wg     sync.WaitGroup
}

type Config struct {
	// SinkPath is the JSONL file path. Caller is responsible for
	// expanding ~ etc.; the sink mkdir's the parent.
	SinkPath string
	// BufferSize bounds the in-flight channel. A burst beyond this
	// drops spans with a slog.Warn (observable, not silent).
	BufferSize int
	// DedupeTTL — how long the dedupe table remembers a signature.
	// Default 24h; tests use shorter.
	DedupeTTL time.Duration
}

// NewObserver assembles the pipeline with the default JSONL-only
// sink at cfg.SinkPath. Use NewObserverWithSink to compose extra
// sinks (e.g. Phase 2 BacklogSink).
func NewObserver(cfg Config) (*Observer, error) {
	sink, err := NewJSONLineSink(cfg.SinkPath)
	if err != nil {
		return nil, err
	}
	return NewObserverWithSink(cfg, sink), nil
}

// NewObserverWithSink lets the caller supply any Sink (including a
// MultiSink composing JSONL + BacklogSink + future variants). The
// caller owns the sink's lifecycle in the sense that Observer.Stop
// will call its Close.
func NewObserverWithSink(cfg Config, sink Sink) *Observer {
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 256
	}
	if cfg.DedupeTTL == 0 {
		cfg.DedupeTTL = 24 * time.Hour
	}
	ch := make(chan rawSpan, cfg.BufferSize)
	return &Observer{
		cfg:        cfg,
		processor:  NewSpanProcessor(ch),
		classifier: &Classifier{},
		dedupe:     NewDedupe(cfg.DedupeTTL, nowFunc),
		sink:       sink,
		ch:         ch,
		stopCh:     make(chan struct{}),
	}
}

// Processor exposes the SpanProcessor for registration on the
// TracerProvider.
func (o *Observer) Processor() *SpanProcessor { return o.processor }

// Start spawns the consumer goroutine. Idempotent enough for serve's
// retry paths: calling Start twice would spawn two consumers, so
// don't.
func (o *Observer) Start(_ context.Context) {
	o.wg.Add(1)
	go o.run()
}

// Stop signals the consumer to drain and exits once the channel is
// empty. Safe to call multiple times.
func (o *Observer) Stop(_ context.Context) error {
	select {
	case <-o.stopCh:
		// already stopped
	default:
		close(o.stopCh)
	}
	o.wg.Wait()
	return o.sink.Close()
}

func (o *Observer) run() {
	defer o.wg.Done()
	for {
		select {
		case <-o.stopCh:
			// Drain whatever's pending so a healthy shutdown doesn't
			// lose observations the SpanProcessor already accepted.
			for {
				select {
				case rs := <-o.ch:
					o.handle(rs)
				default:
					return
				}
			}
		case rs := <-o.ch:
			o.handle(rs)
		}
	}
}

func (o *Observer) handle(rs rawSpan) {
	tool := rs.Attributes[attrToolName]
	if tool == "" {
		// Some exec spans have no tool.name; fall back to binary so the
		// signature stays meaningful (`bash`, `git`, …).
		tool = rs.Attributes[attrExecBinary]
	}
	scope := rs.Name
	if v := rs.Attributes[attrExecScope]; v != "" {
		scope = "ycode.exec." + v
	}
	cat, normalized, sig, qualifies := o.classifier.Qualify(tool, scope, rs.StatusError)
	if !qualifies {
		return
	}
	firstSeen, count := o.dedupe.See(sig)
	if !firstSeen {
		// Already logged this signature in the current TTL window;
		// the running count is implicit in the dedupe table and will
		// be surfaced via ycode selfheal status in Phase 4. Silent
		// here to keep the JSONL one-line-per-distinct-failure.
		return
	}
	dur := rs.EndTime.Sub(rs.StartTime).Milliseconds()
	if v := rs.Attributes[attrExecDurationMs]; v != "" {
		// Trust the explicit attribute over EndTime-StartTime when set.
		// (Some BSPs round timestamps.)
		if d, ok := parseInt64(v); ok {
			dur = d
		}
	} else if v := rs.Attributes[attrToolDurationMs]; v != "" {
		if d, ok := parseInt64(v); ok {
			dur = d
		}
	}
	signal := FailureSignal{
		Timestamp:    nowFunc(),
		Signature:    sig,
		Category:     cat,
		ToolName:     tool,
		Scope:        scope,
		ErrorMessage: truncate(rs.StatusError, errMaxLen),
		Normalized:   truncate(normalized, errMaxLen),
		ExitClass:    rs.Attributes[attrExecExitClass],
		DurationMs:   dur,
		AgentClient:  rs.Attributes[attrAgentClient],
		WrapAgent:    rs.Attributes[attrWrapAgent],
		OccurrenceN:  count,
	}
	if err := o.sink.Record(signal); err != nil {
		slog.Warn("selfheal: sink write failed", "signature", sig, "err", err)
	}
}

// parseInt64 is a fmt-free int64 parse so this file doesn't pull in
// strconv just for the optional duration override.
func parseInt64(s string) (int64, bool) {
	var n int64
	var neg bool
	for i, c := range s {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

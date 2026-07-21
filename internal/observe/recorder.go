package observe

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// defaultFieldCap bounds free-text fields (tool args/results, completion) in
// non-verbose mode so the JSONL stays readable and cheap. Verbose mode lifts it.
const defaultFieldCap = 4096

// Recorder accumulates and emits the per-turn action record. It is safe for
// concurrent use — tool calls within a turn may run in parallel — but a session
// drives it serially at the turn boundary (BeginTurn → SetResponse → AddToolCall
// → next BeginTurn / Finish).
//
// A nil *Recorder is a no-op: every method tolerates a nil receiver so callers
// can wire it unconditionally without guarding each call site.
type Recorder struct {
	mu        sync.Mutex
	w         io.Writer // JSONL sink; nil disables file output
	sessionID string
	verbose   bool         // --trace-verbose: capture full prompt + untruncated text
	cost      CostFunc     // local price table
	tracer    trace.Tracer // optional; nil disables span emission
	now       func() time.Time

	cur     *TurnRecord // the open (unflushed) turn
	curSpan trace.Span
	summary *Summary
	closed  bool
}

// Options configures a Recorder.
type Options struct {
	// Writer is the JSONL sink (typically an actions.jsonl file). May be nil.
	Writer io.Writer
	// SessionID stamps every record.
	SessionID string
	// Verbose enables full-prompt capture and disables field truncation.
	Verbose bool
	// Cost is the price table; DefaultCost is used when nil.
	Cost CostFunc
	// Tracer, when non-nil, emits an `agent.turn` span per turn with tool-call
	// events. Wire it from the OTEL provider; leave nil for JSONL-only.
	Tracer trace.Tracer
}

// New creates a Recorder. The returned recorder writes a session summary when
// Finish is called.
func New(opts Options) *Recorder {
	cost := opts.Cost
	if cost == nil {
		cost = DefaultCost
	}
	return &Recorder{
		w:         opts.Writer,
		sessionID: opts.SessionID,
		verbose:   opts.Verbose,
		cost:      cost,
		tracer:    opts.Tracer,
		now:       time.Now,
		summary: &Summary{
			Kind:      KindSummary,
			SessionID: opts.SessionID,
			PerModel:  map[string]*ModelTotals{},
			PerTool:   map[string]*ToolTotals{},
		},
	}
}

// BeginParams describes the outbound request for a turn.
type BeginParams struct {
	Turn            int
	ServedModel     string // the model the cascade actually used this turn
	BaseModel       string // the configured base model
	Reason          string // escalation reason, when known
	Provider        string
	Temperature     *float64
	TopP            *float64
	MaxTokens       int
	ReasoningEffort string
	// PromptHash is a stable, secret-free reference to the prompt (e.g. the
	// request hash the runtime already computes). Preferred over dumping text.
	PromptHash string
	// Prompt is the full prompt text; stored (redacted) ONLY in verbose mode.
	Prompt string
}

// BeginTurn opens a new turn record. Any previously-open turn is flushed first,
// so a turn with no tool calls (the final answer) is still written on the next
// BeginTurn or at Finish.
func (r *Recorder) BeginTurn(ctx context.Context, p BeginParams) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.flushLocked()

	started := r.now()
	if r.summary.StartedAt.IsZero() {
		r.summary.StartedAt = started
	}
	rec := &TurnRecord{
		Kind:      KindTurn,
		SessionID: r.sessionID,
		Turn:      p.Turn,
		StartedAt: started,
		Request: RequestRecord{
			ServedModel:     p.ServedModel,
			BaseModel:       p.BaseModel,
			Escalated:       escalated(p.ServedModel, p.BaseModel),
			Reason:          p.Reason,
			Provider:        p.Provider,
			Temperature:     p.Temperature,
			TopP:            p.TopP,
			MaxTokens:       p.MaxTokens,
			ReasoningEffort: p.ReasoningEffort,
			PromptHash:      p.PromptHash,
		},
	}
	if rec.Request.PromptHash == "" && p.Prompt != "" {
		rec.Request.PromptHash = hashRef(p.Prompt)
	}
	if r.verbose && p.Prompt != "" {
		rec.Request.Prompt = Redact(p.Prompt)
	}
	r.cur = rec

	if r.tracer != nil {
		_, span := r.tracer.Start(ctx, "agent.turn", trace.WithAttributes(
			attribute.Int("turn", p.Turn),
			attribute.String("served_model", p.ServedModel),
			attribute.String("base_model", p.BaseModel),
			attribute.Bool("escalated", rec.Request.Escalated),
			attribute.String("reason", p.Reason),
			attribute.String("provider", p.Provider),
		))
		r.curSpan = span
	}
}

// ResponseParams describes the inbound response for the open turn.
type ResponseParams struct {
	FinishReason string
	Completion   string
	// ServedModel, when the provider reports a model different from what was
	// requested, updates served_model and re-derives escalated (provider swap).
	ServedModel      string
	PromptTokens     int
	CompletionTokens int
	CacheWriteTokens int
	CacheReadTokens  int
}

// SetResponse fills the response side of the open turn and prices it via the
// local table. No-op if no turn is open.
func (r *Recorder) SetResponse(p ResponseParams) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur == nil {
		return
	}
	if p.ServedModel != "" && p.ServedModel != r.cur.Request.ServedModel {
		// The provider served a model other than the one requested — record the
		// truth and re-derive escalation against the base.
		r.cur.Request.ServedModel = p.ServedModel
		r.cur.Request.Escalated = escalated(p.ServedModel, r.cur.Request.BaseModel)
		if r.curSpan != nil {
			r.curSpan.SetAttributes(
				attribute.String("served_model", p.ServedModel),
				attribute.Bool("escalated", r.cur.Request.Escalated),
			)
		}
	}
	model := r.cur.Request.ServedModel
	cost := r.cost(model, p.PromptTokens, p.CompletionTokens, p.CacheWriteTokens, p.CacheReadTokens)
	r.cur.Request.PromptTokens = p.PromptTokens
	r.cur.Response = ResponseRecord{
		FinishReason:     p.FinishReason,
		Completion:       truncate(Redact(p.Completion), r.fieldCap()),
		CompletionHash:   hashRef(p.Completion),
		PromptTokens:     p.PromptTokens,
		CompletionTokens: p.CompletionTokens,
		CacheWriteTokens: p.CacheWriteTokens,
		CacheReadTokens:  p.CacheReadTokens,
		CostUSD:          cost,
	}
	if r.curSpan != nil {
		r.curSpan.SetAttributes(
			attribute.String("finish_reason", p.FinishReason),
			attribute.Int("prompt_tokens", p.PromptTokens),
			attribute.Int("completion_tokens", p.CompletionTokens),
			attribute.Float64("cost_usd", cost),
		)
	}
}

// ToolParams describes one tool call within the open turn.
type ToolParams struct {
	Name       string
	Arguments  string
	Result     string
	Error      string
	DurationMs int64
}

// AddToolCall appends a tool call to the open turn. Safe under concurrency.
// No-op if no turn is open.
func (r *Recorder) AddToolCall(p ToolParams) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur == nil {
		return
	}
	status := StatusOK
	if p.Error != "" {
		status = StatusError
	}
	fc := r.fieldCap()
	r.cur.ToolCalls = append(r.cur.ToolCalls, ToolCallRecord{
		Name:       p.Name,
		Arguments:  truncate(Redact(p.Arguments), fc),
		Result:     truncate(Redact(p.Result), fc),
		Error:      truncate(Redact(p.Error), fc),
		Status:     status,
		DurationMs: p.DurationMs,
	})
	if r.curSpan != nil {
		r.curSpan.AddEvent("tool.call", trace.WithAttributes(
			attribute.String("tool.name", p.Name),
			attribute.String("tool.status", status),
			attribute.Int64("tool.duration_ms", p.DurationMs),
		))
	}
}

// FlushTurn writes the open turn to the JSONL sink, folds it into the running
// summary, and ends its span. Idempotent: a no-op when no turn is open, so it is
// safe to call from multiple backstops (BeginTurn, end of tool execution,
// Finish).
func (r *Recorder) FlushTurn() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushLocked()
}

// Finish flushes any open turn and writes the session summary as the final
// JSONL line. It returns the summary (also useful in tests) and is idempotent.
func (r *Recorder) Finish() *Summary {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.summary
	}
	r.flushLocked()
	r.summary.EndedAt = r.now()
	r.writeLocked(r.summary)
	r.closed = true
	return r.summary
}

// Verbose reports whether full-prompt capture is enabled (--trace-verbose), so
// callers can skip building an expensive prompt string when it won't be stored.
// Nil-safe.
func (r *Recorder) Verbose() bool {
	return r != nil && r.verbose
}

// Summary returns a snapshot pointer to the running summary. Intended for tests
// and post-run inspection.
func (r *Recorder) Summary() *Summary {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.summary
}

// flushLocked assumes r.mu is held.
func (r *Recorder) flushLocked() {
	if r.cur == nil {
		return
	}
	rec := r.cur
	r.cur = nil
	rec.EndedAt = r.now()
	rec.DurationMs = rec.EndedAt.Sub(rec.StartedAt).Milliseconds()

	r.foldSummary(rec)
	r.writeLocked(rec)

	if r.curSpan != nil {
		r.curSpan.End()
		r.curSpan = nil
	}
}

// foldSummary aggregates one flushed turn into the session totals. Summing here,
// from the exact record being written, is what makes the summary reconcile with
// the per-turn lines by construction.
func (r *Recorder) foldSummary(rec *TurnRecord) {
	s := r.summary
	s.Turns++
	s.PromptTokens += rec.Response.PromptTokens
	s.CompletionTokens += rec.Response.CompletionTokens
	s.CostUSD += rec.Response.CostUSD
	if rec.Request.Escalated {
		s.Escalations++
	}

	model := rec.Request.ServedModel
	mt := s.PerModel[model]
	if mt == nil {
		mt = &ModelTotals{}
		s.PerModel[model] = mt
	}
	mt.Turns++
	mt.PromptTokens += rec.Response.PromptTokens
	mt.CompletionTokens += rec.Response.CompletionTokens
	mt.CostUSD += rec.Response.CostUSD
	if rec.Request.Escalated {
		mt.Escalations++
	}

	for _, tc := range rec.ToolCalls {
		s.ToolCalls++
		tt := s.PerTool[tc.Name]
		if tt == nil {
			tt = &ToolTotals{}
			s.PerTool[tc.Name] = tt
		}
		tt.Calls++
		if tc.Status == StatusError {
			tt.Failures++
			s.ToolFailures++
		}
	}
}

// writeLocked marshals v and writes it as one JSONL line. Assumes r.mu is held.
func (r *Recorder) writeLocked(v any) {
	if r.w == nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = r.w.Write(b)
}

func (r *Recorder) fieldCap() int {
	if r.verbose {
		return 0
	}
	return defaultFieldCap
}

// escalated reports whether a turn ran on a model other than its base. Both must
// be known for the signal to be meaningful.
func escalated(served, base string) bool {
	return served != "" && base != "" && served != base
}

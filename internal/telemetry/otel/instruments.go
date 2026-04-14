package otel

import (
	"go.opentelemetry.io/otel/metric"
)

// Instruments holds all pre-created OTEL metric instruments.
type Instruments struct {
	// LLM metrics.
	LLMCallDuration     metric.Float64Histogram
	LLMCallTotal        metric.Int64Counter
	LLMTokensInput      metric.Int64Counter
	LLMTokensOutput     metric.Int64Counter
	LLMTokensCacheRead  metric.Int64Counter
	LLMTokensCacheWrite metric.Int64Counter
	LLMCostDollars      metric.Float64Counter
	LLMContextUsed      metric.Int64Gauge

	// Tool metrics.
	ToolCallDuration metric.Float64Histogram
	ToolCallTotal    metric.Int64Counter

	// Turn metrics.
	TurnDuration  metric.Float64Histogram
	TurnToolCount metric.Int64Histogram
	SessionTurns  metric.Int64Counter

	// Session metrics.
	SessionDuration  metric.Float64Histogram
	SessionTotalCost metric.Float64Counter
	SessionTokensIn  metric.Int64Counter
	SessionTokensOut metric.Int64Counter

	// Turn-level file change metrics.
	TurnFilesChanged metric.Int64Histogram
	TurnLinesAdded   metric.Int64Histogram
	TurnLinesDeleted metric.Int64Histogram

	// Compaction metrics.
	CompactionTotal       metric.Int64Counter
	CompactionTokensSaved metric.Int64Counter
}

// NewInstruments creates all OTEL metric instruments from the given meter.
func NewInstruments(m metric.Meter) (*Instruments, error) {
	var inst Instruments
	var err error

	if inst.LLMCallDuration, err = m.Float64Histogram("ycode.llm.call.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("LLM API call latency")); err != nil {
		return nil, err
	}
	if inst.LLMCallTotal, err = m.Int64Counter("ycode.llm.call.total",
		metric.WithDescription("Total LLM API calls")); err != nil {
		return nil, err
	}
	if inst.LLMTokensInput, err = m.Int64Counter("ycode.llm.tokens.input",
		metric.WithUnit("tokens"),
		metric.WithDescription("Input tokens consumed")); err != nil {
		return nil, err
	}
	if inst.LLMTokensOutput, err = m.Int64Counter("ycode.llm.tokens.output",
		metric.WithUnit("tokens"),
		metric.WithDescription("Output tokens consumed")); err != nil {
		return nil, err
	}
	if inst.LLMTokensCacheRead, err = m.Int64Counter("ycode.llm.tokens.cache_read",
		metric.WithUnit("tokens"),
		metric.WithDescription("Tokens served from cache")); err != nil {
		return nil, err
	}
	if inst.LLMTokensCacheWrite, err = m.Int64Counter("ycode.llm.tokens.cache_write",
		metric.WithUnit("tokens"),
		metric.WithDescription("Tokens written to cache")); err != nil {
		return nil, err
	}
	if inst.LLMCostDollars, err = m.Float64Counter("ycode.llm.cost.dollars",
		metric.WithUnit("USD"),
		metric.WithDescription("Estimated cumulative cost")); err != nil {
		return nil, err
	}
	if inst.LLMContextUsed, err = m.Int64Gauge("ycode.llm.context_window.used",
		metric.WithUnit("tokens"),
		metric.WithDescription("Tokens used in context window")); err != nil {
		return nil, err
	}
	if inst.ToolCallDuration, err = m.Float64Histogram("ycode.tool.call.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Tool execution latency")); err != nil {
		return nil, err
	}
	if inst.ToolCallTotal, err = m.Int64Counter("ycode.tool.call.total",
		metric.WithDescription("Tool invocations")); err != nil {
		return nil, err
	}
	if inst.TurnDuration, err = m.Float64Histogram("ycode.turn.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Full turn duration")); err != nil {
		return nil, err
	}
	if inst.TurnToolCount, err = m.Int64Histogram("ycode.turn.tool_count",
		metric.WithDescription("Tools invoked per turn")); err != nil {
		return nil, err
	}
	if inst.SessionTurns, err = m.Int64Counter("ycode.session.turns",
		metric.WithDescription("Turns per session")); err != nil {
		return nil, err
	}
	if inst.SessionDuration, err = m.Float64Histogram("ycode.session.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Session duration")); err != nil {
		return nil, err
	}
	if inst.SessionTotalCost, err = m.Float64Counter("ycode.session.cost",
		metric.WithUnit("USD"),
		metric.WithDescription("Cumulative session cost")); err != nil {
		return nil, err
	}
	if inst.SessionTokensIn, err = m.Int64Counter("ycode.session.tokens.input",
		metric.WithUnit("tokens"),
		metric.WithDescription("Session input tokens")); err != nil {
		return nil, err
	}
	if inst.SessionTokensOut, err = m.Int64Counter("ycode.session.tokens.output",
		metric.WithUnit("tokens"),
		metric.WithDescription("Session output tokens")); err != nil {
		return nil, err
	}
	if inst.TurnFilesChanged, err = m.Int64Histogram("ycode.turn.files_changed",
		metric.WithDescription("Files modified per turn")); err != nil {
		return nil, err
	}
	if inst.TurnLinesAdded, err = m.Int64Histogram("ycode.turn.lines_added",
		metric.WithDescription("Lines added per turn")); err != nil {
		return nil, err
	}
	if inst.TurnLinesDeleted, err = m.Int64Histogram("ycode.turn.lines_deleted",
		metric.WithDescription("Lines deleted per turn")); err != nil {
		return nil, err
	}
	if inst.CompactionTotal, err = m.Int64Counter("ycode.compaction.total",
		metric.WithDescription("Compaction events")); err != nil {
		return nil, err
	}
	if inst.CompactionTokensSaved, err = m.Int64Counter("ycode.compaction.tokens_saved",
		metric.WithUnit("tokens"),
		metric.WithDescription("Tokens reclaimed by compaction")); err != nil {
		return nil, err
	}

	return &inst, nil
}

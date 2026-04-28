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

	// Pause/resume metrics.
	PauseTotal    metric.Int64Counter
	PauseDuration metric.Float64Histogram
	ResumeTotal   metric.Int64Counter

	// API error metrics.
	APIErrorTotal            metric.Int64Counter
	MessageStructureWarnings metric.Int64Counter

	// General error metric — unified counter for all error surfaces.
	// Labels: component (conversation, tool, session, tui, subagent, command),
	//         error_type (specific classification), severity (error, warning).
	ErrorTotal metric.Int64Counter

	// Inference engine metrics.
	InferenceCallDuration  metric.Float64Histogram
	InferenceCallTotal     metric.Int64Counter
	InferenceTokensInput   metric.Int64Counter
	InferenceTokensOutput  metric.Int64Counter
	InferenceModelLoadTime metric.Float64Histogram
	InferenceRunnerStarts  metric.Int64Counter
	InferenceRunnerCrashes metric.Int64Counter

	// Memory metrics
	MemoryRecallDuration metric.Float64Histogram
	MemoryRecallTotal    metric.Int64Counter
	MemorySaveTotal      metric.Int64Counter

	// Ralph metrics
	RalphIterationTotal metric.Int64Counter
	RalphIterationScore metric.Float64Histogram
	RalphRunDuration    metric.Float64Histogram

	// DAG metrics
	DAGRunDuration  metric.Float64Histogram
	DAGNodeDuration metric.Float64Histogram

	// Quality metrics
	ToolDegradationTotal metric.Int64Counter

	// Search metrics
	SearchGrepDuration    metric.Float64Histogram
	SearchGrepTotal       metric.Int64Counter
	SearchGrepIndexedHits metric.Int64Counter // queries accelerated by index
	SearchSymbolDuration  metric.Float64Histogram
	SearchSymbolTotal     metric.Int64Counter
	SearchRefGraphTotal   metric.Int64Counter
	SearchTrigramTotal    metric.Int64Counter
	SearchIndexerDuration metric.Float64Histogram
	SearchIndexerFiles    metric.Int64Counter
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

	// Pause/resume instruments.
	if inst.PauseTotal, err = m.Int64Counter("ycode.pause.total",
		metric.WithDescription("Total pause events")); err != nil {
		return nil, err
	}
	if inst.PauseDuration, err = m.Float64Histogram("ycode.pause.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Time spent paused")); err != nil {
		return nil, err
	}
	if inst.ResumeTotal, err = m.Int64Counter("ycode.resume.total",
		metric.WithDescription("Total resume events")); err != nil {
		return nil, err
	}

	// API error instruments.
	if inst.APIErrorTotal, err = m.Int64Counter("ycode.api.error.total",
		metric.WithDescription("API errors by type and status code")); err != nil {
		return nil, err
	}
	if inst.MessageStructureWarnings, err = m.Int64Counter("ycode.message.structure.warnings",
		metric.WithDescription("Message structure validation warnings (orphan tool_call_ids, role adjacency violations)")); err != nil {
		return nil, err
	}

	// General error instrument.
	if inst.ErrorTotal, err = m.Int64Counter("ycode.error.total",
		metric.WithDescription("Errors by component, type, and severity")); err != nil {
		return nil, err
	}

	// Inference engine instruments.
	if inst.InferenceCallDuration, err = m.Float64Histogram("ycode.inference.call.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Local inference call latency")); err != nil {
		return nil, err
	}
	if inst.InferenceCallTotal, err = m.Int64Counter("ycode.inference.call.total",
		metric.WithDescription("Total local inference calls")); err != nil {
		return nil, err
	}
	if inst.InferenceTokensInput, err = m.Int64Counter("ycode.inference.tokens.input",
		metric.WithUnit("tokens"),
		metric.WithDescription("Local inference input tokens")); err != nil {
		return nil, err
	}
	if inst.InferenceTokensOutput, err = m.Int64Counter("ycode.inference.tokens.output",
		metric.WithUnit("tokens"),
		metric.WithDescription("Local inference output tokens")); err != nil {
		return nil, err
	}
	if inst.InferenceModelLoadTime, err = m.Float64Histogram("ycode.inference.model.load_time",
		metric.WithUnit("ms"),
		metric.WithDescription("Model load time")); err != nil {
		return nil, err
	}
	if inst.InferenceRunnerStarts, err = m.Int64Counter("ycode.inference.runner.starts",
		metric.WithDescription("Runner process start count")); err != nil {
		return nil, err
	}
	if inst.InferenceRunnerCrashes, err = m.Int64Counter("ycode.inference.runner.crashes",
		metric.WithDescription("Runner process crash count")); err != nil {
		return nil, err
	}

	// Memory instruments.
	if inst.MemoryRecallDuration, err = m.Float64Histogram("ycode.memory.recall_duration",
		metric.WithUnit("ms")); err != nil {
		return nil, err
	}
	if inst.MemoryRecallTotal, err = m.Int64Counter("ycode.memory.recall_total"); err != nil {
		return nil, err
	}
	if inst.MemorySaveTotal, err = m.Int64Counter("ycode.memory.save_total"); err != nil {
		return nil, err
	}

	// Ralph instruments.
	if inst.RalphIterationTotal, err = m.Int64Counter("ycode.ralph.iteration_total"); err != nil {
		return nil, err
	}
	if inst.RalphIterationScore, err = m.Float64Histogram("ycode.ralph.iteration_score"); err != nil {
		return nil, err
	}
	if inst.RalphRunDuration, err = m.Float64Histogram("ycode.ralph.run_duration",
		metric.WithUnit("ms")); err != nil {
		return nil, err
	}

	// DAG instruments.
	if inst.DAGRunDuration, err = m.Float64Histogram("ycode.dag.run_duration",
		metric.WithUnit("ms")); err != nil {
		return nil, err
	}
	if inst.DAGNodeDuration, err = m.Float64Histogram("ycode.dag.node_duration",
		metric.WithUnit("ms")); err != nil {
		return nil, err
	}

	// Quality instruments.
	if inst.ToolDegradationTotal, err = m.Int64Counter("ycode.tool.degradation_total"); err != nil {
		return nil, err
	}

	// Search metrics.
	if inst.SearchGrepDuration, err = m.Float64Histogram("ycode.search.grep.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Grep search latency")); err != nil {
		return nil, err
	}
	if inst.SearchGrepTotal, err = m.Int64Counter("ycode.search.grep.total",
		metric.WithDescription("Total grep searches")); err != nil {
		return nil, err
	}
	if inst.SearchGrepIndexedHits, err = m.Int64Counter("ycode.search.grep.indexed_hits",
		metric.WithDescription("Grep queries accelerated by Bleve index")); err != nil {
		return nil, err
	}
	if inst.SearchSymbolDuration, err = m.Float64Histogram("ycode.search.symbol.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Symbol search latency")); err != nil {
		return nil, err
	}
	if inst.SearchSymbolTotal, err = m.Int64Counter("ycode.search.symbol.total",
		metric.WithDescription("Total symbol searches")); err != nil {
		return nil, err
	}
	if inst.SearchRefGraphTotal, err = m.Int64Counter("ycode.search.refgraph.total",
		metric.WithDescription("Total reference graph queries")); err != nil {
		return nil, err
	}
	if inst.SearchTrigramTotal, err = m.Int64Counter("ycode.search.trigram.total",
		metric.WithDescription("Total trigram index queries")); err != nil {
		return nil, err
	}
	if inst.SearchIndexerDuration, err = m.Float64Histogram("ycode.search.indexer.duration",
		metric.WithUnit("ms"),
		metric.WithDescription("Background indexer pass latency")); err != nil {
		return nil, err
	}
	if inst.SearchIndexerFiles, err = m.Int64Counter("ycode.search.indexer.files",
		metric.WithDescription("Files indexed by background indexer")); err != nil {
		return nil, err
	}

	return &inst, nil
}

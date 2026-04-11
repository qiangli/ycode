// Package otel provides OpenTelemetry instrumentation for ycode.
package otel

import "go.opentelemetry.io/otel/attribute"

// Semantic attribute keys for LLM observability.
var (
	// LLM call attributes.
	AttrLLMProvider      = attribute.Key("llm.provider")
	AttrLLMModel         = attribute.Key("llm.model")
	AttrLLMMaxTokens     = attribute.Key("llm.max_tokens")
	AttrLLMTemperature   = attribute.Key("llm.temperature")
	AttrLLMTokensInput   = attribute.Key("llm.tokens.input")
	AttrLLMTokensOutput  = attribute.Key("llm.tokens.output")
	AttrLLMTokensTotal   = attribute.Key("llm.tokens.total")
	AttrLLMCacheCreation = attribute.Key("llm.tokens.cache_creation")
	AttrLLMCacheRead     = attribute.Key("llm.tokens.cache_read")
	AttrLLMCostDollars   = attribute.Key("llm.cost.dollars")
	AttrLLMDurationMs    = attribute.Key("llm.duration_ms")
	AttrLLMSuccess       = attribute.Key("llm.success")
	AttrLLMError         = attribute.Key("llm.error")
	AttrLLMStopReason    = attribute.Key("llm.stop_reason")
	AttrLLMStream        = attribute.Key("llm.stream")

	// Session / turn attributes.
	AttrSessionID     = attribute.Key("session.id")
	AttrTurnIndex     = attribute.Key("turn.index")
	AttrTurnToolCalls = attribute.Key("turn.tool_calls")
	AttrTurnToolNames = attribute.Key("turn.tool_names")

	// Tool call attributes.
	AttrToolName          = attribute.Key("tool.name")
	AttrToolSource        = attribute.Key("tool.source")
	AttrToolCategory      = attribute.Key("tool.category")
	AttrToolInputSummary  = attribute.Key("tool.input.summary")
	AttrToolOutputSummary = attribute.Key("tool.output.summary")
	AttrToolOutputSize    = attribute.Key("tool.output.size")
	AttrToolSuccess       = attribute.Key("tool.success")
	AttrToolError         = attribute.Key("tool.error")
	AttrToolDurationMs    = attribute.Key("tool.duration_ms")

	// Compaction attributes.
	AttrTokensBefore = attribute.Key("tokens.before")
	AttrTokensAfter  = attribute.Key("tokens.after")
)

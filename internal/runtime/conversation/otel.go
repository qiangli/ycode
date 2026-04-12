package conversation

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/api"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// OTELConfig holds optional OTEL instrumentation for the conversation runtime.
type OTELConfig struct {
	Tracer     trace.Tracer
	Inst       *yotel.Instruments
	ReqLogger  *yotel.RequestLogger
	ConvLogger *yotel.ConversationLogger
	Provider   string // provider kind for logging
}

// SetOTEL configures OTEL instrumentation on the runtime.
func (r *Runtime) SetOTEL(cfg *OTELConfig) {
	r.otel = cfg
}

// InstrumentedTurn wraps Turn() with an OTEL span and metrics.
func (r *Runtime) InstrumentedTurn(ctx context.Context, messages []api.Message, turnIndex int) (*TurnResult, error) {
	if r.otel == nil || r.otel.Tracer == nil {
		return r.Turn(ctx, messages)
	}

	ctx, span := r.otel.Tracer.Start(ctx, "ycode.conversation.turn",
		trace.WithAttributes(
			attribute.Int("turn.index", turnIndex),
		),
	)
	defer span.End()

	start := time.Now()
	result, err := r.Turn(ctx, messages)
	dur := time.Since(start)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return result, err
	}

	// Set turn attributes.
	toolNames := make([]string, len(result.ToolCalls))
	for i, tc := range result.ToolCalls {
		toolNames[i] = tc.Name
	}
	span.SetAttributes(
		attribute.Int("turn.tool_calls", len(result.ToolCalls)),
		attribute.String("turn.tool_names", strings.Join(toolNames, ",")),
		attribute.String("llm.stop_reason", result.StopReason),
		attribute.Int("llm.tokens.input", result.Usage.InputTokens+result.Usage.PromptTokens),
		attribute.Int("llm.tokens.output", result.Usage.OutputTokens+result.Usage.CompletionTokens),
	)

	// Record metrics.
	if r.otel.Inst != nil {
		r.otel.Inst.TurnDuration.Record(ctx, float64(dur.Milliseconds()))
		r.otel.Inst.TurnToolCount.Record(ctx, int64(len(result.ToolCalls)))
		r.otel.Inst.SessionTurns.Add(ctx, 1)
	}

	return result, nil
}

// InstrumentedTurnWithRecovery wraps TurnWithRecovery with compaction span and metrics.
func (r *Runtime) InstrumentedTurnWithRecovery(ctx context.Context, messages []api.Message, turnIndex int) (*TurnResult, *RecoveryResult, error) {
	if r.otel == nil || r.otel.Tracer == nil {
		return r.TurnWithRecovery(ctx, messages)
	}

	ctx, span := r.otel.Tracer.Start(ctx, "ycode.conversation.turn_with_recovery",
		trace.WithAttributes(attribute.Int("turn.index", turnIndex)),
	)
	defer span.End()

	result, recovery, err := r.TurnWithRecovery(ctx, messages)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	if recovery != nil && r.otel.Inst != nil {
		if recovery.CompactedCount > 0 || recovery.Flushed {
			span.AddEvent("ycode.compaction", trace.WithAttributes(
				attribute.Int("compacted_count", recovery.CompactedCount),
				attribute.Int("preserved_count", recovery.PreservedCount),
				attribute.Bool("flushed", recovery.Flushed),
			))
			r.otel.Inst.CompactionTotal.Add(ctx, 1)
		}
		if recovery.PrunedCount > 0 {
			span.AddEvent("ycode.pruning", trace.WithAttributes(
				attribute.Int("pruned_count", recovery.PrunedCount),
			))
		}
	}

	return result, recovery, err
}

// LogConversation logs a full conversation record via the RequestLogger and
// emits structured OTEL log records for VictoriaLogs.
func (r *Runtime) LogConversation(turnIndex int, req *api.Request, result *TurnResult, toolResults []ToolCall, err error) {
	if r.otel == nil || (r.otel.ReqLogger == nil && r.otel.ConvLogger == nil) {
		return
	}

	record := &yotel.ConversationRecord{
		Timestamp:    time.Now(),
		SessionID:    r.session.ID,
		TurnIndex:    turnIndex,
		Provider:     r.otel.Provider,
		Model:        r.config.Model,
		SystemPrompt: req.System,
		ToolDefs:     len(req.Tools),
		MaxTokens:    req.MaxTokens,
		Temperature:  req.Temperature,
		Success:      err == nil,
	}

	if result != nil {
		record.ResponseText = result.TextContent
		record.ThinkingContent = result.ThinkingContent
		record.StopReason = result.StopReason
		record.TokensIn = result.Usage.InputTokens + result.Usage.PromptTokens
		record.TokensOut = result.Usage.OutputTokens + result.Usage.CompletionTokens
		record.CacheCreation = result.Usage.CacheCreationInput
		record.CacheRead = result.Usage.CacheReadInput
		record.DurationMs = result.Duration.Milliseconds()
		record.EstimatedCostUSD = yotel.EstimateCost(r.config.Model,
			record.TokensIn, record.TokensOut, record.CacheCreation, record.CacheRead)

		for _, tc := range toolResults {
			record.ToolCalls = append(record.ToolCalls, yotel.ToolCallLog{
				Name:    tc.Name,
				Input:   tc.Input,
				Output:  tc.Result,
				Error:   tc.Error,
				Success: tc.Error == "",
			})
		}
	}

	if err != nil {
		record.Error = err.Error()
	}

	// Messages are serialized as raw JSON to avoid import cycles.
	// This is best-effort — if it fails, we still log without messages.
	if r.otel.ReqLogger != nil {
		_ = r.otel.ReqLogger.Log(record)
	}

	// Emit structured OTEL log records for VictoriaLogs.
	if r.otel.ConvLogger != nil {
		r.otel.ConvLogger.LogConversation(record)
		for _, tc := range record.ToolCalls {
			r.otel.ConvLogger.LogToolCall(record.SessionID, record.TurnIndex, tc)
		}
	}
}

// recordTurnMetrics records per-turn LLM metrics via OTEL instruments.
func (r *Runtime) recordTurnMetrics(ctx context.Context, result *TurnResult) {
	if r.otel == nil || r.otel.Inst == nil || result == nil {
		return
	}
	inst := r.otel.Inst
	attrs := metric.WithAttributes(
		attribute.String("llm.model", r.config.Model),
	)
	inputTokens := int64(result.Usage.InputTokens + result.Usage.PromptTokens)
	outputTokens := int64(result.Usage.OutputTokens + result.Usage.CompletionTokens)

	inst.LLMCallDuration.Record(ctx, float64(result.Duration.Milliseconds()), attrs)
	inst.LLMCallTotal.Add(ctx, 1, attrs)
	if inputTokens > 0 {
		inst.LLMTokensInput.Add(ctx, inputTokens, attrs)
	}
	if outputTokens > 0 {
		inst.LLMTokensOutput.Add(ctx, outputTokens, attrs)
	}
	if result.Usage.CacheReadInput > 0 {
		inst.LLMTokensCacheRead.Add(ctx, int64(result.Usage.CacheReadInput), attrs)
	}
	if result.Usage.CacheCreationInput > 0 {
		inst.LLMTokensCacheWrite.Add(ctx, int64(result.Usage.CacheCreationInput), attrs)
	}
	cost := yotel.EstimateCost(r.config.Model, int(inputTokens), int(outputTokens),
		result.Usage.CacheCreationInput, result.Usage.CacheReadInput)
	if cost > 0 {
		inst.LLMCostDollars.Add(ctx, cost, attrs)
	}
}

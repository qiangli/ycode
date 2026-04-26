package conversation

import (
	"context"
	"fmt"
	"log/slog"
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

// messageStructure captures the shape of a message list for diagnostics.
type messageStructure struct {
	RoleSequence    string   // e.g. "user,assistant,user,assistant,user"
	MessageCount    int      // total messages
	ToolUseIDs      []string // tool_use IDs in assistant messages
	ToolResultIDs   []string // tool_result IDs in user messages
	OrphanUseIDs    []string // tool_use IDs without matching tool_result
	OrphanResultIDs []string // tool_result IDs without matching tool_use
}

// analyzeMessageStructure inspects messages for tool_use/tool_result adjacency issues.
func analyzeMessageStructure(messages []api.Message) messageStructure {
	ms := messageStructure{MessageCount: len(messages)}
	roles := make([]string, len(messages))
	for i, m := range messages {
		roles[i] = string(m.Role)
		for _, b := range m.Content {
			if b.Type == api.ContentTypeToolUse {
				ms.ToolUseIDs = append(ms.ToolUseIDs, b.ID)
			}
			if b.Type == api.ContentTypeToolResult {
				ms.ToolResultIDs = append(ms.ToolResultIDs, b.ToolUseID)
			}
		}
	}
	ms.RoleSequence = strings.Join(roles, ",")

	// Find orphans.
	resultSet := make(map[string]bool, len(ms.ToolResultIDs))
	for _, id := range ms.ToolResultIDs {
		resultSet[id] = true
	}
	useSet := make(map[string]bool, len(ms.ToolUseIDs))
	for _, id := range ms.ToolUseIDs {
		useSet[id] = true
		if !resultSet[id] {
			ms.OrphanUseIDs = append(ms.OrphanUseIDs, id)
		}
	}
	for _, id := range ms.ToolResultIDs {
		if !useSet[id] {
			ms.OrphanResultIDs = append(ms.OrphanResultIDs, id)
		}
	}
	return ms
}

// classifyAPIError extracts structured error info from an API error string.
// Returns (errorType, statusCode, detail).
func classifyAPIError(errMsg string) (errorType string, statusCode string, detail string) {
	errorType = "unknown"
	statusCode = "0"
	detail = errMsg

	if strings.Contains(errMsg, "400") {
		statusCode = "400"
	} else if strings.Contains(errMsg, "401") {
		statusCode = "401"
	} else if strings.Contains(errMsg, "429") {
		statusCode = "429"
	} else if strings.Contains(errMsg, "500") {
		statusCode = "500"
	} else if strings.Contains(errMsg, "529") {
		statusCode = "529"
	}

	if strings.Contains(errMsg, "tool_call_id") || strings.Contains(errMsg, "tool_calls") {
		errorType = "orphan_tool_call"
		detail = "tool_use blocks missing matching tool_result responses"
	} else if strings.Contains(errMsg, "invalid_request_error") {
		errorType = "invalid_request"
	} else if strings.Contains(errMsg, "overloaded") {
		errorType = "overloaded"
	} else if strings.Contains(errMsg, "rate_limit") {
		errorType = "rate_limit"
	} else if strings.Contains(errMsg, "authentication") || strings.Contains(errMsg, "401") {
		errorType = "auth"
	}
	return
}

// InstrumentedTurn wraps Turn() with an OTEL span and metrics.
func (r *Runtime) InstrumentedTurn(ctx context.Context, messages []api.Message, turnIndex int) (*TurnResult, error) {
	if r.otel == nil || r.otel.Tracer == nil {
		return r.Turn(ctx, messages)
	}

	// Pre-flight: validate message structure and log warnings.
	ms := analyzeMessageStructure(messages)
	if len(ms.OrphanUseIDs) > 0 {
		slog.Warn("conversation.message_structure.orphan_tool_use",
			"turn", turnIndex,
			"orphan_ids", strings.Join(ms.OrphanUseIDs, ","),
			"role_sequence", ms.RoleSequence,
			"message_count", ms.MessageCount,
			"tool_use_count", len(ms.ToolUseIDs),
			"tool_result_count", len(ms.ToolResultIDs),
			"session", r.session.ID,
		)
		if r.otel.Inst != nil {
			r.otel.Inst.MessageStructureWarnings.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("warning_type", "orphan_tool_use"),
					attribute.Int("orphan_count", len(ms.OrphanUseIDs)),
				))
		}
	}

	ctx, span := r.otel.Tracer.Start(ctx, "ycode.conversation.turn",
		trace.WithAttributes(
			attribute.Int("turn.index", turnIndex),
			attribute.Int("message.count", ms.MessageCount),
			attribute.Int("message.tool_use_count", len(ms.ToolUseIDs)),
			attribute.Int("message.tool_result_count", len(ms.ToolResultIDs)),
			attribute.Int("message.orphan_tool_use_count", len(ms.OrphanUseIDs)),
		),
	)
	defer span.End()

	if len(ms.OrphanUseIDs) > 0 {
		span.AddEvent("ycode.message_structure.warning", trace.WithAttributes(
			attribute.StringSlice("orphan_tool_use_ids", ms.OrphanUseIDs),
			attribute.String("role_sequence", ms.RoleSequence),
		))
	}

	start := time.Now()
	result, err := r.Turn(ctx, messages)
	dur := time.Since(start)

	if err != nil {
		errMsg := err.Error()
		errorType, statusCode, detail := classifyAPIError(errMsg)
		span.RecordError(err)
		span.SetStatus(codes.Error, errMsg)
		span.SetAttributes(
			attribute.String("error.type", errorType),
			attribute.String("error.status_code", statusCode),
			attribute.String("error.detail", detail),
			attribute.String("message.role_sequence", ms.RoleSequence),
		)

		// Record API error metric.
		if r.otel.Inst != nil {
			r.otel.Inst.APIErrorTotal.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("error_type", errorType),
					attribute.String("status_code", statusCode),
				))
		}

		// Emit structured error log for VictoriaLogs.
		if r.otel.ConvLogger != nil {
			r.otel.ConvLogger.LogAPIError(r.session.ID, turnIndex, errorType, statusCode, detail, ms.RoleSequence,
				len(ms.ToolUseIDs), len(ms.ToolResultIDs), ms.OrphanUseIDs)
		}

		slog.Error("conversation.api_error",
			"turn", turnIndex,
			"error_type", errorType,
			"status_code", statusCode,
			"detail", detail,
			"role_sequence", ms.RoleSequence,
			"tool_use_count", len(ms.ToolUseIDs),
			"tool_result_count", len(ms.ToolResultIDs),
			"orphan_tool_use_ids", fmt.Sprintf("%v", ms.OrphanUseIDs),
			"session", r.session.ID,
		)

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

	// Record turn-level metrics.
	if r.otel.Inst != nil {
		r.otel.Inst.TurnDuration.Record(ctx, float64(dur.Milliseconds()))
		r.otel.Inst.TurnToolCount.Record(ctx, int64(len(result.ToolCalls)))
		r.otel.Inst.SessionTurns.Add(ctx, 1)
	}

	// Record LLM call metrics (tokens, cost, latency).
	result.Duration = dur
	r.recordTurnMetrics(ctx, result)

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

// recordError records a general error via metric, structured log, and slog.
// component: "conversation", "tool", "session", "tui", "subagent", "command"
// errorType: specific classification (e.g. "execution_failure", "io_failure")
// detail: tool name, file path, or other identifying context
// err: the actual error
func (r *Runtime) recordError(ctx context.Context, component, errorType, detail string, err error) {
	if r.otel == nil {
		return
	}
	if r.otel.Inst != nil {
		r.otel.Inst.ErrorTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("component", component),
				attribute.String("error_type", errorType),
			))
	}
	sessionID := ""
	if r.session != nil {
		sessionID = r.session.ID
	}
	if r.otel.ConvLogger != nil {
		r.otel.ConvLogger.LogError(component, errorType, detail+": "+err.Error(), sessionID)
	}
	slog.Error("ycode.error",
		"component", component,
		"error_type", errorType,
		"detail", detail,
		"error", err.Error(),
		"session", sessionID,
	)
}

package otel

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// ConversationLogger emits structured OTEL log records for conversations
// and tool calls, routed to VictoriaLogs via the OTEL collector pipeline.
type ConversationLogger struct {
	logger     log.Logger
	instanceID string
}

// NewConversationLogger creates a logger that emits conversation and tool call
// records as structured OTEL log records with proper metadata attributes.
func NewConversationLogger(provider *sdklog.LoggerProvider, instanceID string) *ConversationLogger {
	return &ConversationLogger{
		logger:     provider.Logger("ycode.conversation"),
		instanceID: instanceID,
	}
}

// LogConversation emits a conversation turn as a structured OTEL log record.
func (cl *ConversationLogger) LogConversation(record *ConversationRecord) {
	if record == nil {
		return
	}

	// Build body: combine system prompt, messages, and response for full context.
	body := record.ResponseText
	if len(record.Messages) > 0 {
		body = string(record.Messages)
	}

	var rec log.Record
	rec.SetTimestamp(record.Timestamp)
	rec.SetSeverityText("INFO")
	rec.SetSeverity(log.SeverityInfo)
	rec.SetBody(attribute.StringValue(body))
	rec.AddAttributes(
		attribute.String("log.type", "conversation"),
		attribute.String("session.id", record.SessionID),
		attribute.String("instance.id", cl.instanceID),
		attribute.Int("turn.index", record.TurnIndex),
		attribute.String("llm.model", record.Model),
		attribute.String("llm.provider", record.Provider),
		attribute.String("llm.stop_reason", record.StopReason),
		attribute.Int("llm.tokens.input", record.TokensIn),
		attribute.Int("llm.tokens.output", record.TokensOut),
		attribute.Int("llm.tokens.cache_creation", record.CacheCreation),
		attribute.Int("llm.tokens.cache_read", record.CacheRead),
		attribute.Float64("llm.cost.dollars", record.EstimatedCostUSD),
		attribute.Int64("duration_ms", record.DurationMs),
		attribute.Bool("success", record.Success),
		attribute.Int("tool_defs", record.ToolDefs),
		attribute.Int("tool_calls", len(record.ToolCalls)),
	)
	if record.Error != "" {
		rec.AddAttributes(attribute.String("error", record.Error))
	}
	if record.SystemPrompt != "" {
		rec.AddAttributes(attribute.String("system_prompt", truncate(record.SystemPrompt, 4096)))
	}

	cl.logger.Emit(context.Background(), rec)
}

// LogToolCall emits a tool call as a structured OTEL log record.
func (cl *ConversationLogger) LogToolCall(sessionID string, turnIndex int, tc ToolCallLog) {
	var rec log.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverityText("INFO")
	rec.SetSeverity(log.SeverityInfo)

	// Body: combine input and output for full tool call context.
	body := tc.Output
	if len(tc.Input) > 0 {
		body = string(tc.Input)
		if tc.Output != "" {
			body += "\n---\n" + tc.Output
		}
	}
	rec.SetBody(attribute.StringValue(body))

	rec.AddAttributes(
		attribute.String("log.type", "tool_call"),
		attribute.String("session.id", sessionID),
		attribute.String("instance.id", cl.instanceID),
		attribute.Int("turn.index", turnIndex),
		attribute.String("tool.name", tc.Name),
		attribute.Bool("tool.success", tc.Success),
		attribute.Int64("tool.duration_ms", tc.DurationMs),
	)
	if tc.Source != "" {
		rec.AddAttributes(attribute.String("tool.source", tc.Source))
	}
	if tc.Error != "" {
		rec.AddAttributes(attribute.String("error", tc.Error))
	}

	// Include full input/output as separate attributes for structured querying.
	if len(tc.Input) > 0 {
		rec.AddAttributes(attribute.String("tool.input", string(tc.Input)))
	}
	if tc.Output != "" {
		rec.AddAttributes(attribute.String("tool.output", tc.Output))
	}

	cl.logger.Emit(context.Background(), rec)
}

// LogAPIError emits an API error as a structured OTEL log record with full
// message structure context for debugging tool_use/tool_result adjacency issues.
func (cl *ConversationLogger) LogAPIError(sessionID string, turnIndex int,
	errorType, statusCode, detail, roleSequence string,
	toolUseCount, toolResultCount int, orphanUseIDs []string) {

	var rec log.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverityText("ERROR")
	rec.SetSeverity(log.SeverityError)
	rec.SetBody(attribute.StringValue(detail))
	rec.AddAttributes(
		attribute.String("log.type", "api_error"),
		attribute.String("session.id", sessionID),
		attribute.String("instance.id", cl.instanceID),
		attribute.Int("turn.index", turnIndex),
		attribute.String("error.type", errorType),
		attribute.String("error.status_code", statusCode),
		attribute.String("error.detail", detail),
		attribute.String("message.role_sequence", roleSequence),
		attribute.Int("message.tool_use_count", toolUseCount),
		attribute.Int("message.tool_result_count", toolResultCount),
	)
	if len(orphanUseIDs) > 0 {
		rec.AddAttributes(attribute.String("message.orphan_tool_use_ids", strings.Join(orphanUseIDs, ",")))
	}

	cl.logger.Emit(context.Background(), rec)
}

// LogError emits a general error as a structured OTEL log record.
// Use this for any error surface — tool execution, session I/O, commands,
// subagent failures, TUI errors. The component field identifies the origin.
func (cl *ConversationLogger) LogError(component, errorType, detail, sessionID string, extra ...attribute.KeyValue) {
	var rec log.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverityText("ERROR")
	rec.SetSeverity(log.SeverityError)
	rec.SetBody(attribute.StringValue(detail))
	rec.AddAttributes(
		attribute.String("log.type", "error"),
		attribute.String("error.component", component),
		attribute.String("error.type", errorType),
		attribute.String("error.detail", detail),
		attribute.String("instance.id", cl.instanceID),
	)
	if sessionID != "" {
		rec.AddAttributes(attribute.String("session.id", sessionID))
	}
	rec.AddAttributes(extra...)

	cl.logger.Emit(context.Background(), rec)
}

// SessionSummary holds aggregate data for a completed session.
type SessionSummary struct {
	SessionID    string
	Title        string
	TurnsCount   int
	TokensIn     int
	TokensOut    int
	TotalCostUSD float64
	DurationMs   int64
	FilesChanged int
	ToolsUsed    []string
	Success      bool
	Error        string
}

// LogSessionSummary emits a session summary as a structured OTEL log record.
// Called when a session ends or the user exits.
func (cl *ConversationLogger) LogSessionSummary(summary *SessionSummary) {
	if summary == nil {
		return
	}

	var rec log.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverityText("INFO")
	rec.SetSeverity(log.SeverityInfo)
	rec.SetBody(attribute.StringValue("session_summary"))
	rec.AddAttributes(
		attribute.String("log.type", "session_summary"),
		attribute.String("session.id", summary.SessionID),
		attribute.String("instance.id", cl.instanceID),
		attribute.Int("session.turns", summary.TurnsCount),
		attribute.Int("session.tokens.input", summary.TokensIn),
		attribute.Int("session.tokens.output", summary.TokensOut),
		attribute.Float64("session.cost_usd", summary.TotalCostUSD),
		attribute.Int64("session.duration_ms", summary.DurationMs),
		attribute.Int("session.files_changed", summary.FilesChanged),
		attribute.Bool("success", summary.Success),
	)
	if summary.Title != "" {
		rec.AddAttributes(attribute.String("session.title", summary.Title))
	}
	if summary.Error != "" {
		rec.AddAttributes(attribute.String("error", summary.Error))
	}
	if len(summary.ToolsUsed) > 0 {
		toolsJSON, _ := json.Marshal(summary.ToolsUsed)
		rec.AddAttributes(attribute.String("session.tools_used", string(toolsJSON)))
	}

	cl.logger.Emit(context.Background(), rec)
}

// LogChatMessage emits an individual chat message as a structured OTEL log record.
func (cl *ConversationLogger) LogChatMessage(sessionID string, turnIndex int, role string, content json.RawMessage) {
	var rec log.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverityText("INFO")
	rec.SetSeverity(log.SeverityInfo)
	rec.SetBody(attribute.StringValue(string(content)))
	rec.AddAttributes(
		attribute.String("log.type", "chat_message"),
		attribute.String("session.id", sessionID),
		attribute.String("instance.id", cl.instanceID),
		attribute.Int("turn.index", turnIndex),
		attribute.String("message.role", role),
	)

	cl.logger.Emit(context.Background(), rec)
}

package otel

import (
	"context"
	"encoding/json"
	"time"

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
	rec.SetBody(log.StringValue(body))
	rec.AddAttributes(
		log.String("log.type", "conversation"),
		log.String("session.id", record.SessionID),
		log.String("instance.id", cl.instanceID),
		log.Int("turn.index", record.TurnIndex),
		log.String("llm.model", record.Model),
		log.String("llm.provider", record.Provider),
		log.String("llm.stop_reason", record.StopReason),
		log.Int("llm.tokens.input", record.TokensIn),
		log.Int("llm.tokens.output", record.TokensOut),
		log.Int("llm.tokens.cache_creation", record.CacheCreation),
		log.Int("llm.tokens.cache_read", record.CacheRead),
		log.Float64("llm.cost.dollars", record.EstimatedCostUSD),
		log.Int64("duration_ms", record.DurationMs),
		log.Bool("success", record.Success),
		log.Int("tool_defs", record.ToolDefs),
		log.Int("tool_calls", len(record.ToolCalls)),
	)
	if record.Error != "" {
		rec.AddAttributes(log.String("error", record.Error))
	}
	if record.SystemPrompt != "" {
		rec.AddAttributes(log.String("system_prompt", truncate(record.SystemPrompt, 4096)))
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
	rec.SetBody(log.StringValue(body))

	rec.AddAttributes(
		log.String("log.type", "tool_call"),
		log.String("session.id", sessionID),
		log.String("instance.id", cl.instanceID),
		log.Int("turn.index", turnIndex),
		log.String("tool.name", tc.Name),
		log.Bool("tool.success", tc.Success),
		log.Int64("tool.duration_ms", tc.DurationMs),
	)
	if tc.Source != "" {
		rec.AddAttributes(log.String("tool.source", tc.Source))
	}
	if tc.Error != "" {
		rec.AddAttributes(log.String("error", tc.Error))
	}

	// Include full input/output as separate attributes for structured querying.
	if len(tc.Input) > 0 {
		rec.AddAttributes(log.String("tool.input", string(tc.Input)))
	}
	if tc.Output != "" {
		rec.AddAttributes(log.String("tool.output", tc.Output))
	}

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
	rec.SetBody(log.StringValue("session_summary"))
	rec.AddAttributes(
		log.String("log.type", "session_summary"),
		log.String("session.id", summary.SessionID),
		log.String("instance.id", cl.instanceID),
		log.Int("session.turns", summary.TurnsCount),
		log.Int("session.tokens.input", summary.TokensIn),
		log.Int("session.tokens.output", summary.TokensOut),
		log.Float64("session.cost_usd", summary.TotalCostUSD),
		log.Int64("session.duration_ms", summary.DurationMs),
		log.Int("session.files_changed", summary.FilesChanged),
		log.Bool("success", summary.Success),
	)
	if summary.Title != "" {
		rec.AddAttributes(log.String("session.title", summary.Title))
	}
	if summary.Error != "" {
		rec.AddAttributes(log.String("error", summary.Error))
	}
	if len(summary.ToolsUsed) > 0 {
		toolsJSON, _ := json.Marshal(summary.ToolsUsed)
		rec.AddAttributes(log.String("session.tools_used", string(toolsJSON)))
	}

	cl.logger.Emit(context.Background(), rec)
}

// LogChatMessage emits an individual chat message as a structured OTEL log record.
func (cl *ConversationLogger) LogChatMessage(sessionID string, turnIndex int, role string, content json.RawMessage) {
	var rec log.Record
	rec.SetTimestamp(time.Now())
	rec.SetSeverityText("INFO")
	rec.SetSeverity(log.SeverityInfo)
	rec.SetBody(log.StringValue(string(content)))
	rec.AddAttributes(
		log.String("log.type", "chat_message"),
		log.String("session.id", sessionID),
		log.String("instance.id", cl.instanceID),
		log.Int("turn.index", turnIndex),
		log.String("message.role", role),
	)

	cl.logger.Emit(context.Background(), rec)
}

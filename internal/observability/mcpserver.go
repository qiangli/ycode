package observability

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/tools"
)

// TelemetryHandler implements mcp.ServerHandler, exposing ycode's
// traces, logs, and metrics to external AI agents via MCP protocol.
type TelemetryHandler struct {
	// StateFunc returns a real-time TUI state dump (optional).
	StateFunc func() string
}

// NewTelemetryHandler creates a handler that serves ycode telemetry via MCP.
// The otelDataDir and sqlStore must be configured via tools.SetOTELDataDir
// and tools.SetMetricsStore before this handler is used.
func NewTelemetryHandler() *TelemetryHandler {
	return &TelemetryHandler{}
}

func (h *TelemetryHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "query_traces",
			Description: "Query OTEL trace spans. Find slow operations, errors, or get execution flow summary. query_type: recent_spans, slow_spans, error_spans, summary. Optional: limit (int), threshold_ms (int), session_id (string).",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"query_type": {"type": "string", "enum": ["recent_spans", "slow_spans", "error_spans", "summary"]},
					"limit": {"type": "integer"},
					"threshold_ms": {"type": "integer"},
					"session_id": {"type": "string"}
				},
				"required": ["query_type"]
			}`),
		},
		{
			Name:        "query_logs",
			Description: "Query conversation logs. Review turns, find errors, search responses, or analyze cost. query_type: recent_turns, turn_errors, search, cost_summary. Optional: limit (int), pattern (string for search), session_id (string).",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"query_type": {"type": "string", "enum": ["recent_turns", "turn_errors", "search", "cost_summary"]},
					"limit": {"type": "integer"},
					"pattern": {"type": "string"},
					"session_id": {"type": "string"}
				},
				"required": ["query_type"]
			}`),
		},
		{
			Name:        "query_metrics",
			Description: "Query tool execution metrics. Analyze performance, failures, and usage patterns. query_type: tool_stats, recent_failures, session_summary, slow_tools. Optional: limit (int), session_id (string), threshold_ms (int).",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"query_type": {"type": "string", "enum": ["tool_stats", "recent_failures", "session_summary", "slow_tools"]},
					"limit": {"type": "integer"},
					"session_id": {"type": "string"},
					"threshold_ms": {"type": "integer"}
				},
				"required": ["query_type"]
			}`),
		},
	}
}

func (h *TelemetryHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "query_traces":
		return tools.QueryTraces(ctx, input)
	case "query_logs":
		return tools.QueryLogs(ctx, input)
	case "query_metrics":
		return tools.QueryMetrics(ctx, input)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *TelemetryHandler) ListResources() []mcp.Resource {
	resources := []mcp.Resource{
		{
			URI:         "state://tui",
			Name:        "TUI State",
			Description: "Real-time snapshot of the ycode TUI state machine (working, paused, confirming, channels, etc.)",
			MimeType:    "text/plain",
		},
	}
	return resources
}

func (h *TelemetryHandler) ReadResource(ctx context.Context, uri string) (string, error) {
	switch uri {
	case "state://tui":
		if h.StateFunc != nil {
			return h.StateFunc(), nil
		}
		return "TUI state not available (no active TUI instance connected).", nil
	default:
		return "", fmt.Errorf("unknown resource: %s", uri)
	}
}

func mustJSON(s string) json.RawMessage {
	var v json.RawMessage
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(fmt.Sprintf("invalid JSON in MCP schema: %v", err))
	}
	return v
}

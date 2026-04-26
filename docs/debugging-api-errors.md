# Debugging Errors with Observability

How to diagnose and fix any error in ycode using the embedded observability stack. Includes a worked example and a repeatable playbook for new error surfaces.

## Playbook: Any Error

### Step 1: Start with `diagnose_errors` (MCP) or Error Overview Dashboard

**For AI agents** — call the general diagnostic tool first:

```json
{"tool": "diagnose_errors", "input": {}}
```

This queries all error metrics, recent error logs, and firing alerts in one call. Filter by component if you know the area:

```json
{"tool": "diagnose_errors", "input": {"component": "tool"}}
{"tool": "diagnose_errors", "input": {"component": "conversation", "session_id": "abc123"}}
```

Components: `conversation`, `tool`, `session`, `tui`, `subagent`, `command`.

**For humans** — open `http://127.0.0.1:58080/dashboard/` and check **ycode > Error Overview**:

| Panel | What to look for |
|-------|------------------|
| Error Rate by Component | Which component is producing errors |
| Error Rate by Type | Specific error classification |
| Errors by Component (table) | Total counts for triage |
| Tool Errors / Conversation Errors | Drilldown timeseries |

### Step 2: Check Structured Logs (VictoriaLogs)

Open `http://127.0.0.1:58080/logs/select/vmui/` or use MCP:

```json
{"tool": "search_victorialogs", "input": {"query": "log.type:error"}}
```

**General error log fields** (present on all `log.type:error` records):

| Field | Description |
|-------|-------------|
| `error.component` | Which subsystem: `conversation`, `tool`, `session`, `tui`, `subagent`, `command` |
| `error.type` | Specific classification: `execution_failure`, `orphan_tool_call`, `io_failure`, etc. |
| `error.detail` | Human-readable explanation |
| `session.id` | For correlation across events |
| `instance.id` | Which ycode instance |

**Useful LogsQL queries:**

```logsql
# All errors
log.type:error

# Errors in a specific component
error.component:tool

# API-specific errors with message structure context
log.type:api_error AND error.type:orphan_tool_call

# Errors in a specific session
log.type:error AND session.id:abc123

# All error-level slog entries
ycode.error
```

### Step 3: Check Traces (Jaeger)

Open `http://127.0.0.1:58080/traces/` and search for error spans:

- **Service**: `ycode`
- **Tags**: `error=true`

Error spans carry structured attributes:

| Attribute | Description |
|-----------|-------------|
| `error.type` | Classification matching the metric label |
| `error.status_code` | HTTP status code (for API errors) |
| `message.orphan_tool_use_count` | Pre-flight validation result |
| `message.role_sequence` | Message structure snapshot |
| `tools.failed` | Count of failed tools in a batch |

### Step 4: Check Alerts (AlertManager)

Open `http://127.0.0.1:58080/alerts/` or use MCP:

```json
{"tool": "list_alerts", "input": {}}
```

**Default alert rules:**

| Alert | Severity | Fires when |
|-------|----------|------------|
| `YcodeErrorRateElevated` | warning | >0.5 errors/s across any component for 2m |
| `YcodeComponentErrors` | critical | >1 error/s in a single component for 5m |
| `YcodeToolExecutionFailures` | warning | >0.2 tool errors/s for 3m |
| `YcodeOrphanToolCall` | critical | Any message structure violation (immediate) |
| `YcodeAPIError400Burst` | critical | >0.5 400-errors/s for 1m |
| `YcodeAPIErrorRate` | warning | Elevated API error rate for 2m |
| `YcodeLLMCallFailureRate` | warning | >10% LLM call failure rate for 5m |

### Step 5: Drill Down with Specialized Tools (MCP)

After the general `diagnose_errors` identifies the component, use the specialized tools:

```json
// API errors — message structure, orphan tool_call_ids, error classification
{"tool": "diagnose_api_errors", "input": {"error_type": "orphan_tool_call"}}

// Pause/resume — duration, deferred context, correlation with errors
{"tool": "diagnose_pause_resume", "input": {}}

// Message structure — orphan IDs, role sequence, resolution guide
{"tool": "diagnose_message_structure", "input": {}}

// Ad-hoc PromQL
{"tool": "promql_query", "input": {"query": "sum(ycode_error_total) by (component, error_type)"}}

// Raw log search
{"tool": "search_victorialogs", "input": {"query": "error.component:tool AND error.type:execution_failure"}}
```

## Worked Example: Pause/Resume API 400 Error

**Symptom**: User pauses, adds context, resumes, sees:

```
Error: stream: API error 400: tool_call_ids did not have response messages: bash:66
```

**Diagnosis with `diagnose_errors`:**
1. `ycode_error_total{component="conversation", error_type="turn_failure"}` spiking
2. `ycode_api_error_total{error_type="orphan_tool_call", status_code="400"}` > 0
3. `ycode_message_structure_warnings{warning_type="orphan_tool_use"}` > 0
4. `YcodeOrphanToolCall` alert firing

**Drill down with `diagnose_api_errors`:**
- Error log shows `message.role_sequence: user,assistant,assistant,user,user`
- `message.orphan_tool_use_ids: bash:66`
- `message.tool_use_count: 1, tool_result_count: 0`

**Root cause**: Context injected while paused (assistant+user message pair) broke tool_use/tool_result adjacency.

**Fix**: Defer context to `pausedContext` when `pausedCalls > 0`, pre-load into `midTurnCh` on resume (`internal/cli/tui.go`).

## Error Surfaces and What They Emit

| Component | Error Types | Metric | Log Type | Where |
|-----------|-------------|--------|----------|-------|
| `conversation` | `orphan_tool_call`, `invalid_request`, `rate_limit`, `overloaded`, `auth` | `ycode.api.error.total` + `ycode.error.total` | `api_error` + `error` | `conversation/otel.go` |
| `conversation` | `turn_failure` | `ycode.error.total` | slog.Error | `cli/tui.go` |
| `tool` | `execution_failure` | `ycode.error.total` | `error` | `conversation/runtime.go` |
| `session` | (pending) | (pending) | (pending) | `cli/tui.go` |
| `subagent` | (via span) | (via tool metric) | span error | `conversation/spawner.go` |
| `command` | (pending) | (pending) | (pending) | `runtime/builtin/` |

## Adding Observability to a New Error Surface

### The `recordError` pattern

The runtime provides a general-purpose error recording function:

```go
r.recordError(ctx, "component", "error_type", "detail", err)
```

This emits three things in one call:
1. **Metric**: `ycode.error.total{component="...", error_type="..."}`
2. **Structured log**: `log.type:error` to VictoriaLogs via `ConversationLogger.LogError()`
3. **slog.Error**: for local console/file output with structured fields

For TUI-level errors (no runtime access), use the instruments directly:

```go
if inst := m.otelInst(); inst != nil {
    inst.ErrorTotal.Add(ctx, 1, metric.WithAttributes(
        attribute.String("component", "tui"),
        attribute.String("error_type", "my_error"),
    ))
}
tuiLogger.Error("tui.my_error", "error", err.Error(), "session", m.app.session.ID)
```

### Instrumentation Checklist

For every new error surface, complete this checklist:

```
[ ] 1. Call recordError() or emit ErrorTotal metric with component + error_type labels
[ ] 2. Emit structured log via ConversationLogger.LogError() or slog.Error with structured fields
[ ] 3. Add span attributes/events on relevant trace spans
[ ] 4. Add dashboard panel to default_project.json (Error Overview or component-specific)
[ ] 5. Add alert rule to alerts.go if the error is actionable
[ ] 6. Verify diagnose_errors MCP tool captures the new error type
[ ] 7. Update instruments_test.go for new instruments
```

### Data Flow

```
          Code (any component)
               │
    ┌──────────┼──────────┐
    │          │          │
    ▼          ▼          ▼
 ErrorTotal  LogError  slog.Error
 (metric)   (OTEL log) (console)
    │          │
    ▼          ▼
 Prometheus  VictoriaLogs
    │          │
    ├──→ Perses dashboards (Error Overview, API Health, component-specific)
    ├──→ AlertManager (ycode-api-health rules, ycode-general-errors rules)
    │          │
    └──────────┴──→ MCP diagnose_errors (queries both in one call)
```

### Key Files

| File | Purpose |
|------|---------|
| `internal/telemetry/otel/instruments.go` | All metric instruments including `ErrorTotal` |
| `internal/telemetry/otel/conversation_logger.go` | `LogError()` — general structured log to VictoriaLogs |
| `internal/runtime/conversation/otel.go` | `recordError()` — unified error recording; `classifyAPIError()`, `analyzeMessageStructure()` |
| `internal/observability/dashboards/default_project.json` | Perses dashboards: Error Overview, API Health, Pause & Resume |
| `internal/observability/dashboards/alerts.go` | Default alert rules: general + API-specific |
| `internal/observability/mcpserver.go` | MCP tools: `diagnose_errors`, `diagnose_api_errors`, `diagnose_pause_resume`, `diagnose_message_structure` |
| `internal/cli/tui.go` | TUI-level error recording and pause/resume instrumentation |

## MCP Diagnostic Tool Reference

| Tool | Use when | Queries |
|------|----------|---------|
| `diagnose_errors` | First response to any error | Error metrics + error logs + alerts + TUI state |
| `diagnose_api_errors` | API 400/429/500 errors | API error metrics + api_error logs + pause correlation |
| `diagnose_pause_resume` | Errors after pause/resume | Pause metrics + pause logs + correlated API errors |
| `diagnose_message_structure` | `tool_call_id` errors | Structure warnings + orphan IDs + resolution guide |
| `promql_query` | Ad-hoc metric investigation | Any PromQL expression |
| `search_victorialogs` | Ad-hoc log search | Any LogsQL query |
| `query_traces` | Span-level investigation | Recent/slow/error spans |
| `list_alerts` | Check firing alerts | AlertManager API |

## API Error Classification Reference

| Error Type | Status Code | Typical Cause |
|-----------|-------------|---------------|
| `orphan_tool_call` | 400 | Message structure: tool_use without tool_result |
| `invalid_request` | 400 | Other malformed request |
| `rate_limit` | 429 | Too many requests |
| `overloaded` | 529 | Provider at capacity |
| `auth` | 401 | Invalid API key |
| `unknown` | varies | Uncategorized |

## General Error Type Reference

| Component | Error Type | Typical Cause |
|-----------|------------|---------------|
| `tool` | `execution_failure` | Tool invocation returned an error |
| `conversation` | `turn_failure` | LLM turn failed (API error, timeout, etc.) |
| `conversation` | `orphan_tool_call` | Message structure violation |
| `session` | `io_failure` | Session file write failed |
| `subagent` | `iteration_exceeded` | Subagent hit max iterations |
| `command` | `execution_failure` | Builtin command (commit, etc.) failed |

package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/observability/dashboards"
	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/tools"
)

// TelemetryHandler implements mcp.ServerHandler, exposing ycode's full
// observability stack to external AI agents via MCP protocol.
// Agents can query traces/logs/metrics, create dashboards, run PromQL,
// search logs, and manage alerts — making the entire stack programmable.
type TelemetryHandler struct {
	// ProxyBaseURL is the base URL for the reverse proxy (e.g. "http://127.0.0.1:58080").
	ProxyBaseURL string
	// PersesDataDir is the Perses file database directory for dashboard creation.
	PersesDataDir string
	// AlertRulesDir is the directory containing Prometheus alert rule YAML files.
	AlertRulesDir string
	// StateFunc returns a real-time TUI state dump (optional).
	StateFunc func() string
}

// NewTelemetryHandler creates a handler that serves the full observability stack via MCP.
func NewTelemetryHandler(proxyBaseURL, persesDataDir, alertRulesDir string) *TelemetryHandler {
	return &TelemetryHandler{
		ProxyBaseURL:  proxyBaseURL,
		PersesDataDir: persesDataDir,
		AlertRulesDir: alertRulesDir,
	}
}

func (h *TelemetryHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		// --- Telemetry query tools (existing) ---
		{
			Name:        "query_traces",
			Description: "Query OTEL trace spans from ycode instances. Find slow operations, errors, or get execution summary. query_type: recent_spans, slow_spans, error_spans, summary.",
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
			Description: "Query conversation logs from ycode instances. Review turns, find errors, search responses, analyze cost. query_type: recent_turns, turn_errors, search, cost_summary.",
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
			Description: "Query tool execution metrics from ycode instances. Analyze performance, failures, usage patterns. query_type: tool_stats, recent_failures, session_summary, slow_tools.",
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

		// --- Perses dashboard tools ---
		{
			Name:        "create_dashboard",
			Description: "Create a new Perses dashboard with PromQL panels. The dashboard appears immediately in the Perses UI. Each panel has a title, PromQL query, and type (timeseries, stat, or table).",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"project": {"type": "string", "description": "Project name (e.g. 'ycode'). Created if it doesn't exist."},
					"name": {"type": "string", "description": "Dashboard display name (e.g. 'LLM Cost Analysis')"},
					"panels": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"title": {"type": "string", "description": "Panel title"},
								"query": {"type": "string", "description": "PromQL query"},
								"type": {"type": "string", "enum": ["timeseries", "stat", "table"], "description": "Panel type (default: timeseries)"}
							},
							"required": ["title", "query"]
						},
						"description": "List of panels to include"
					}
				},
				"required": ["project", "name", "panels"]
			}`),
		},
		{
			Name:        "list_dashboards",
			Description: "List all dashboards in a Perses project.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"project": {"type": "string", "description": "Project name (e.g. 'ycode')"}
				},
				"required": ["project"]
			}`),
		},

		// --- Prometheus query tools ---
		{
			Name:        "promql_query",
			Description: "Execute a PromQL query against Prometheus. Returns current metric values or time series data. Use for ad-hoc metric analysis, debugging, or dashboard prototyping.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "PromQL expression (e.g. 'rate(ycode_llm_call_total[5m])')"},
					"time": {"type": "string", "description": "Evaluation timestamp (RFC3339, optional — defaults to now)"}
				},
				"required": ["query"]
			}`),
		},
		{
			Name:        "promql_query_range",
			Description: "Execute a PromQL range query for time series data over a period. Returns multiple data points.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "PromQL expression"},
					"start": {"type": "string", "description": "Start time (RFC3339 or relative like '-1h')"},
					"end": {"type": "string", "description": "End time (RFC3339, default: now)"},
					"step": {"type": "string", "description": "Query step (e.g. '15s', '1m', default: '15s')"}
				},
				"required": ["query"]
			}`),
		},
		{
			Name:        "list_prometheus_metrics",
			Description: "List all available Prometheus metric names. Useful for discovering what metrics are being collected.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {}
			}`),
		},

		// --- VictoriaLogs query tools ---
		{
			Name:        "search_victorialogs",
			Description: "Search logs in VictoriaLogs using LogsQL. Query structured OTEL logs from all ycode instances.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "LogsQL query (e.g. 'error AND service_name:ycode')"},
					"limit": {"type": "integer", "description": "Max results (default: 50)"},
					"start": {"type": "string", "description": "Start time (e.g. '-1h', RFC3339)"},
					"end": {"type": "string", "description": "End time (default: now)"}
				},
				"required": ["query"]
			}`),
		},

		// --- Alertmanager tools ---
		{
			Name:        "list_alerts",
			Description: "List currently firing alerts from Alertmanager.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {}
			}`),
		},
		{
			Name:        "list_alert_rules",
			Description: "List all configured Prometheus alert rules from YAML files.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {}
			}`),
		},
		{
			Name:        "create_alert_rule",
			Description: "Create a new Prometheus alert rule. The rule is written to a YAML file and becomes active on the next Prometheus evaluation cycle. Use PromQL for the expression.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"name": {"type": "string", "description": "Alert name in PascalCase (e.g. 'YcodeHighToolLatency')"},
					"expr": {"type": "string", "description": "PromQL expression that fires when true (e.g. 'rate(ycode_tool_call_duration_sum[5m]) > 10000')"},
					"for": {"type": "string", "description": "Duration the condition must hold before firing (e.g. '5m', '1m'). Default: '5m'"},
					"severity": {"type": "string", "enum": ["info", "warning", "critical"], "description": "Alert severity. Default: 'warning'"},
					"summary": {"type": "string", "description": "Short human-readable summary"},
					"description": {"type": "string", "description": "Detailed description with optional template variables ({{ $value }}, {{ $labels }})"},
					"group": {"type": "string", "description": "Rule group name. Default: 'ycode-dynamic'"}
				},
				"required": ["name", "expr", "summary"]
			}`),
		},
		{
			Name:        "delete_alert_rule",
			Description: "Delete a Prometheus alert rule by name from the dynamic rules file.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"name": {"type": "string", "description": "Alert name to delete"}
				},
				"required": ["name"]
			}`),
		},
		{
			Name:        "push_alert",
			Description: "Push an alert directly to Alertmanager. Use for immediate notifications without waiting for rule evaluation.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"name": {"type": "string", "description": "Alert name"},
					"severity": {"type": "string", "enum": ["info", "warning", "critical"], "description": "Alert severity"},
					"summary": {"type": "string", "description": "Alert summary"},
					"description": {"type": "string", "description": "Alert description"},
					"labels": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Additional labels"}
				},
				"required": ["name", "summary"]
			}`),
		},
	}
}

func (h *TelemetryHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	// Telemetry queries.
	case "query_traces":
		return tools.QueryTraces(ctx, input)
	case "query_logs":
		return tools.QueryLogs(ctx, input)
	case "query_metrics":
		return tools.QueryMetrics(ctx, input)

	// Perses dashboards.
	case "create_dashboard":
		return h.handleCreateDashboard(input)
	case "list_dashboards":
		return h.handleListDashboards(input)

	// Prometheus.
	case "promql_query":
		return h.handlePromQLQuery(input)
	case "promql_query_range":
		return h.handlePromQLQueryRange(input)
	case "list_prometheus_metrics":
		return h.handleListMetrics()

	// VictoriaLogs.
	case "search_victorialogs":
		return h.handleSearchVictoriaLogs(input)

	// Alertmanager.
	case "list_alerts":
		return h.handleListAlerts()
	case "list_alert_rules":
		return h.handleListAlertRules()
	case "create_alert_rule":
		return h.handleCreateAlertRule(input)
	case "delete_alert_rule":
		return h.handleDeleteAlertRule(input)
	case "push_alert":
		return h.handlePushAlert(input)

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *TelemetryHandler) ListResources() []mcp.Resource {
	return []mcp.Resource{
		{
			URI:         "state://tui",
			Name:        "TUI State",
			Description: "Real-time snapshot of the ycode TUI state machine",
			MimeType:    "text/plain",
		},
	}
}

func (h *TelemetryHandler) ReadResource(ctx context.Context, uri string) (string, error) {
	switch uri {
	case "state://tui":
		if h.StateFunc != nil {
			return h.StateFunc(), nil
		}
		return "TUI state not available.", nil
	default:
		return "", fmt.Errorf("unknown resource: %s", uri)
	}
}

// --- Perses dashboard handlers ---

func (h *TelemetryHandler) handleCreateDashboard(input json.RawMessage) (string, error) {
	if h.PersesDataDir == "" {
		return "", fmt.Errorf("Perses data directory not configured")
	}
	var params struct {
		Project string                      `json:"project"`
		Name    string                      `json:"name"`
		Panels  []dashboards.SimplifiedPanel `json:"panels"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse create_dashboard input: %w", err)
	}
	if params.Project == "" || params.Name == "" || len(params.Panels) == 0 {
		return "", fmt.Errorf("project, name, and at least one panel are required")
	}

	if err := dashboards.CreateDashboard(h.PersesDataDir, params.Project, params.Name, params.Panels, true); err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/dashboard/projects/%s/dashboards", h.ProxyBaseURL, params.Project)
	return fmt.Sprintf("Dashboard %q created in project %q with %d panels.\nView at: %s", params.Name, params.Project, len(params.Panels), url), nil
}

func (h *TelemetryHandler) handleListDashboards(input json.RawMessage) (string, error) {
	var params struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse list_dashboards input: %w", err)
	}
	resp, err := h.proxyGet(fmt.Sprintf("/dashboard/api/v1/projects/%s/dashboards", params.Project))
	if err != nil {
		return "", fmt.Errorf("list dashboards: %w", err)
	}
	return resp, nil
}

// --- Prometheus handlers ---

func (h *TelemetryHandler) handlePromQLQuery(input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
		Time  string `json:"time"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse promql_query input: %w", err)
	}
	path := fmt.Sprintf("/prometheus/api/v1/query?query=%s", params.Query)
	if params.Time != "" {
		path += "&time=" + params.Time
	}
	return h.proxyGet(path)
}

func (h *TelemetryHandler) handlePromQLQueryRange(input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
		Start string `json:"start"`
		End   string `json:"end"`
		Step  string `json:"step"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse promql_query_range input: %w", err)
	}
	if params.Start == "" {
		params.Start = time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	}
	if params.End == "" {
		params.End = time.Now().Format(time.RFC3339)
	}
	if params.Step == "" {
		params.Step = "15s"
	}
	path := fmt.Sprintf("/prometheus/api/v1/query_range?query=%s&start=%s&end=%s&step=%s",
		params.Query, params.Start, params.End, params.Step)
	return h.proxyGet(path)
}

func (h *TelemetryHandler) handleListMetrics() (string, error) {
	return h.proxyGet("/prometheus/api/v1/label/__name__/values")
}

// --- VictoriaLogs handlers ---

func (h *TelemetryHandler) handleSearchVictoriaLogs(input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
		Start string `json:"start"`
		End   string `json:"end"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse search_victorialogs input: %w", err)
	}
	if params.Limit <= 0 {
		params.Limit = 50
	}
	path := fmt.Sprintf("/logs/select/logsql/query?query=%s&limit=%d", params.Query, params.Limit)
	if params.Start != "" {
		path += "&start=" + params.Start
	}
	if params.End != "" {
		path += "&end=" + params.End
	}
	return h.proxyGet(path)
}

// --- Alertmanager handlers ---

func (h *TelemetryHandler) handleListAlerts() (string, error) {
	return h.proxyGet("/alerts/api/v2/alerts")
}

func (h *TelemetryHandler) handleListAlertRules() (string, error) {
	if h.AlertRulesDir == "" {
		return h.proxyGet("/prometheus/api/v1/rules")
	}
	// Read all YAML files in the alerts directory.
	entries, err := filepath.Glob(filepath.Join(h.AlertRulesDir, "*.yml"))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("Alert Rules:\n")
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", filepath.Base(path), string(data))
	}
	if b.Len() == len("Alert Rules:\n") {
		return "No alert rules configured.", nil
	}
	return b.String(), nil
}

func (h *TelemetryHandler) handleCreateAlertRule(input json.RawMessage) (string, error) {
	if h.AlertRulesDir == "" {
		return "", fmt.Errorf("alert rules directory not configured")
	}
	var params struct {
		Name        string `json:"name"`
		Expr        string `json:"expr"`
		For         string `json:"for"`
		Severity    string `json:"severity"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Group       string `json:"group"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse create_alert_rule input: %w", err)
	}
	if params.For == "" {
		params.For = "5m"
	}
	if params.Severity == "" {
		params.Severity = "warning"
	}
	if params.Group == "" {
		params.Group = "ycode-dynamic"
	}
	if params.Description == "" {
		params.Description = params.Summary
	}

	// Build the rule YAML.
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "groups:\n")
	fmt.Fprintf(&buf, "  - name: %s\n", params.Group)
	fmt.Fprintf(&buf, "    rules:\n")
	fmt.Fprintf(&buf, "      - alert: %s\n", params.Name)
	fmt.Fprintf(&buf, "        expr: |\n")
	for _, line := range strings.Split(params.Expr, "\n") {
		fmt.Fprintf(&buf, "          %s\n", line)
	}
	fmt.Fprintf(&buf, "        for: %s\n", params.For)
	fmt.Fprintf(&buf, "        labels:\n")
	fmt.Fprintf(&buf, "          severity: %s\n", params.Severity)
	fmt.Fprintf(&buf, "        annotations:\n")
	fmt.Fprintf(&buf, "          summary: %q\n", params.Summary)
	fmt.Fprintf(&buf, "          description: %q\n", params.Description)

	// Write to a dedicated file for this rule.
	if err := os.MkdirAll(h.AlertRulesDir, 0o755); err != nil {
		return "", err
	}
	filename := strings.ToLower(params.Name) + ".yml"
	path := filepath.Join(h.AlertRulesDir, filename)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("write alert rule: %w", err)
	}

	return fmt.Sprintf("Alert rule %q created at %s\nSeverity: %s | For: %s\nExpr: %s", params.Name, path, params.Severity, params.For, params.Expr), nil
}

func (h *TelemetryHandler) handleDeleteAlertRule(input json.RawMessage) (string, error) {
	if h.AlertRulesDir == "" {
		return "", fmt.Errorf("alert rules directory not configured")
	}
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse delete_alert_rule input: %w", err)
	}
	filename := strings.ToLower(params.Name) + ".yml"
	path := filepath.Join(h.AlertRulesDir, filename)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("alert rule %q not found", params.Name)
		}
		return "", err
	}
	return fmt.Sprintf("Alert rule %q deleted.", params.Name), nil
}

func (h *TelemetryHandler) handlePushAlert(input json.RawMessage) (string, error) {
	var params struct {
		Name        string            `json:"name"`
		Severity    string            `json:"severity"`
		Summary     string            `json:"summary"`
		Description string            `json:"description"`
		Labels      map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse push_alert input: %w", err)
	}
	if params.Severity == "" {
		params.Severity = "warning"
	}
	if params.Description == "" {
		params.Description = params.Summary
	}

	// Build Alertmanager API v2 alert payload.
	labels := map[string]string{
		"alertname": params.Name,
		"severity":  params.Severity,
	}
	for k, v := range params.Labels {
		labels[k] = v
	}
	alert := []map[string]any{
		{
			"labels": labels,
			"annotations": map[string]string{
				"summary":     params.Summary,
				"description": params.Description,
			},
			"startsAt": time.Now().UTC().Format(time.RFC3339),
		},
	}

	body, _ := json.Marshal(alert)
	url := h.ProxyBaseURL + "/alerts/api/v2/alerts"
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("push alert: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("alertmanager returned %d: %s", resp.StatusCode, string(respBody))
	}

	return fmt.Sprintf("Alert %q pushed to Alertmanager (severity: %s)", params.Name, params.Severity), nil
}

// --- HTTP proxy helper ---

func (h *TelemetryHandler) proxyGet(path string) (string, error) {
	url := h.ProxyBaseURL + path
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, path, truncateStr(string(body), 500))
	}
	return string(body), nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func mustJSON(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	var v json.RawMessage
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(fmt.Sprintf("invalid JSON in MCP schema: %v", err))
	}
	return v
}

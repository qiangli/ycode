package dashboards

import (
	"fmt"
	"os"
	"path/filepath"
)

// defaultAlertRules contains the embedded Prometheus alert rules YAML
// for ycode's core error detection and health monitoring.
const defaultAlertRules = `groups:
  - name: ycode-api-health
    rules:
      - alert: YcodeAPIErrorRate
        expr: sum(rate(ycode_api_error_total[5m])) > 0.1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "ycode API error rate elevated"
          description: "API errors detected at {{ $value | printf \"%.2f\" }}/s. Check error_type label for details: orphan_tool_call, invalid_request, rate_limit."

      - alert: YcodeOrphanToolCall
        expr: sum(ycode_message_structure_warnings{warning_type="orphan_tool_use"}) > 0
        for: 0m
        labels:
          severity: critical
        annotations:
          summary: "Message structure violation: orphan tool_call_id detected"
          description: "tool_use blocks exist without matching tool_result responses. This causes API 400 errors. Check pause/resume context injection logic. Query VictoriaLogs: log.type:api_error AND error.type:orphan_tool_call"

      - alert: YcodeAPIError400Burst
        expr: sum(rate(ycode_api_error_total{status_code="400"}[2m])) > 0.5
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Burst of API 400 errors"
          description: "{{ $value | printf \"%.1f\" }} 400 errors/s in the last 2m. Most common cause: message structure violations after pause/resume with pending tool calls."

      - alert: YcodeHighPauseDuration
        expr: histogram_quantile(0.95, rate(ycode_pause_duration_bucket[10m])) > 300000
        for: 5m
        labels:
          severity: info
        annotations:
          summary: "Users spending >5min paused"
          description: "95th percentile pause duration is {{ $value | printf \"%.0f\" }}ms. Extended pauses with pending tool calls increase risk of context staleness."

      - alert: YcodeLLMCallFailureRate
        expr: sum(rate(ycode_api_error_total[5m])) / sum(rate(ycode_llm_call_total[5m])) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "LLM call failure rate above 10%%"
          description: "{{ $value | printf \"%.1f%%\" }} of LLM calls are failing. Check ycode_api_error_total by error_type for breakdown."

  - name: ycode-general-errors
    rules:
      - alert: YcodeErrorRateElevated
        expr: sum(rate(ycode_error_total[5m])) > 0.5
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "ycode error rate elevated across components"
          description: "{{ $value | printf \"%.2f\" }} errors/s. Breakdown by component: use diagnose_errors MCP tool or check Error Overview dashboard."

      - alert: YcodeToolExecutionFailures
        expr: sum(rate(ycode_error_total{component="tool"}[5m])) > 0.2
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "Tool execution failures detected"
          description: "{{ $value | printf \"%.2f\" }} tool errors/s. Check which tools are failing: sum(ycode_error_total{component='tool'}) by (error_type)"

      - alert: YcodeComponentErrors
        expr: sum by (component) (rate(ycode_error_total[5m])) > 1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High error rate in {{ $labels.component }} component"
          description: "{{ $labels.component }} has {{ $value | printf \"%.1f\" }} errors/s for 5+ minutes. Use diagnose_errors with component={{ $labels.component }} for details."
`

// ProvisionAlertRules writes default alert rules to the rules directory
// if they don't already exist. Called during stack initialization.
func ProvisionAlertRules(rulesDir string) error {
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return fmt.Errorf("create alert rules dir: %w", err)
	}

	path := filepath.Join(rulesDir, "ycode-api-health.yml")
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}

	if err := os.WriteFile(path, []byte(defaultAlertRules), 0o644); err != nil {
		return fmt.Errorf("write default alert rules: %w", err)
	}
	return nil
}

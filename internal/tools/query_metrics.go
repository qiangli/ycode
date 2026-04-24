package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/storage"
)

// metricsStore is the module-level SQL store, set via SetMetricsStore.
var metricsStore storage.SQLStore

// metricsSessionID is the current session ID, set via SetMetricsSessionID.
var metricsSessionID string

// SetMetricsStore injects the SQL store for the query_metrics tool.
func SetMetricsStore(s storage.SQLStore) {
	metricsStore = s
}

// SetMetricsSessionID sets the current session ID for "current" session filtering.
func SetMetricsSessionID(id string) {
	metricsSessionID = id
}

// RegisterQueryMetricsHandler wires up the query_metrics tool handler.
func RegisterQueryMetricsHandler(r *Registry) {
	if spec, ok := r.Get("query_metrics"); ok {
		spec.Handler = handleQueryMetrics
	}
}

func checkMetricsStore() error {
	if metricsStore == nil {
		return fmt.Errorf("metrics store is not initialized (SQLite may still be loading)")
	}
	return nil
}

type queryMetricsInput struct {
	QueryType   string `json:"query_type"`
	SessionID   string `json:"session_id"`
	Limit       int    `json:"limit"`
	ThresholdMs int    `json:"threshold_ms"`
}

func handleQueryMetrics(ctx context.Context, input json.RawMessage) (string, error) {
	if err := checkMetricsStore(); err != nil {
		return "", err
	}

	var params queryMetricsInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse query_metrics input: %w", err)
	}

	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}
	if params.SessionID == "" || params.SessionID == "current" {
		params.SessionID = metricsSessionID
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	switch params.QueryType {
	case "tool_stats":
		return queryToolStats(ctx, params)
	case "recent_failures":
		return queryRecentFailures(ctx, params)
	case "session_summary":
		return querySessionSummary(ctx, params)
	case "slow_tools":
		return querySlowTools(ctx, params)
	default:
		return "", fmt.Errorf("unknown query_type: %q (valid: tool_stats, recent_failures, session_summary, slow_tools)", params.QueryType)
	}
}

func queryToolStats(ctx context.Context, p queryMetricsInput) (string, error) {
	query := `SELECT tool_name,
	       COUNT(*)                                       AS total,
	       ROUND(AVG(duration_ms), 1)                     AS avg_ms,
	       MAX(duration_ms)                                AS max_ms,
	       ROUND(100.0 * SUM(success) / COUNT(*), 1)      AS success_pct
	FROM tool_usage`
	var args []any
	if p.SessionID != "all" {
		query += " WHERE session_id = ?"
		args = append(args, p.SessionID)
	}
	query += " GROUP BY tool_name ORDER BY total DESC LIMIT ?"
	args = append(args, p.Limit)

	rows, err := metricsStore.Query(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("query tool_stats: %w", err)
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString("Tool Usage Stats:\n")
	b.WriteString("Tool | Calls | Avg(ms) | Max(ms) | Success%\n")
	b.WriteString("---|---|---|---|---\n")
	count := 0
	for rows.Next() {
		var name string
		var total int
		var avgMs, maxMs, successPct float64
		if err := rows.Scan(&name, &total, &avgMs, &maxMs, &successPct); err != nil {
			return "", fmt.Errorf("scan tool_stats: %w", err)
		}
		fmt.Fprintf(&b, "%s | %d | %.1f | %.0f | %.1f%%\n", name, total, avgMs, maxMs, successPct)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if count == 0 {
		return "No tool usage data found.", nil
	}
	return b.String(), nil
}

func queryRecentFailures(ctx context.Context, p queryMetricsInput) (string, error) {
	query := `SELECT tool_name, duration_ms, timestamp
	FROM tool_usage
	WHERE success = 0`
	var args []any
	if p.SessionID != "all" {
		query += " AND session_id = ?"
		args = append(args, p.SessionID)
	}
	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, p.Limit)

	rows, err := metricsStore.Query(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("query recent_failures: %w", err)
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString("Recent Failures:\n")
	count := 0
	for rows.Next() {
		var name string
		var durationMs int
		var ts string
		if err := rows.Scan(&name, &durationMs, &ts); err != nil {
			return "", fmt.Errorf("scan recent_failures: %w", err)
		}
		fmt.Fprintf(&b, "- %s at %s (%dms)\n", name, ts, durationMs)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if count == 0 {
		return "No failures found.", nil
	}
	return b.String(), nil
}

func querySessionSummary(ctx context.Context, p queryMetricsInput) (string, error) {
	if p.SessionID == "all" {
		return "", fmt.Errorf("session_summary requires a specific session (omit session_id for current)")
	}

	var title, model, createdAt string
	var tokenIn, tokenOut int
	err := metricsStore.QueryRow(ctx,
		`SELECT title, model, created_at, token_input, token_output
		 FROM sessions WHERE id = ?`, p.SessionID).Scan(&title, &model, &createdAt, &tokenIn, &tokenOut)
	if err != nil {
		return "", fmt.Errorf("session not found: %w", err)
	}

	rows, err := metricsStore.Query(ctx,
		`SELECT tool_name, COUNT(*), SUM(CASE WHEN success=0 THEN 1 ELSE 0 END)
		 FROM tool_usage WHERE session_id = ?
		 GROUP BY tool_name ORDER BY COUNT(*) DESC`, p.SessionID)
	if err != nil {
		return "", fmt.Errorf("query session tools: %w", err)
	}
	defer rows.Close()

	var b strings.Builder
	fmt.Fprintf(&b, "Session: %s\n", p.SessionID)
	if title != "" {
		fmt.Fprintf(&b, "Title: %s\n", title)
	}
	fmt.Fprintf(&b, "Model: %s | Created: %s\n", model, createdAt)
	fmt.Fprintf(&b, "Tokens: %d in / %d out (total: %d)\n\n", tokenIn, tokenOut, tokenIn+tokenOut)
	b.WriteString("Tool Breakdown:\n")

	toolCount := 0
	for rows.Next() {
		var name string
		var total, failures int
		if err := rows.Scan(&name, &total, &failures); err != nil {
			return "", fmt.Errorf("scan session tools: %w", err)
		}
		if failures > 0 {
			fmt.Fprintf(&b, "- %s: %d calls (%d failed)\n", name, total, failures)
		} else {
			fmt.Fprintf(&b, "- %s: %d calls\n", name, total)
		}
		toolCount++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if toolCount == 0 {
		b.WriteString("- (no tool usage recorded)\n")
	}
	return b.String(), nil
}

func querySlowTools(ctx context.Context, p queryMetricsInput) (string, error) {
	threshold := p.ThresholdMs
	if threshold <= 0 {
		threshold = 5000
	}

	query := `SELECT tool_name, duration_ms, success, timestamp
	FROM tool_usage
	WHERE duration_ms >= ?`
	args := []any{threshold}
	if p.SessionID != "all" {
		query += " AND session_id = ?"
		args = append(args, p.SessionID)
	}
	query += " ORDER BY duration_ms DESC LIMIT ?"
	args = append(args, p.Limit)

	rows, err := metricsStore.Query(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("query slow_tools: %w", err)
	}
	defer rows.Close()

	var b strings.Builder
	fmt.Fprintf(&b, "Slow Tools (>%dms):\n", threshold)
	count := 0
	for rows.Next() {
		var name string
		var durationMs, success int
		var ts string
		if err := rows.Scan(&name, &durationMs, &success, &ts); err != nil {
			return "", fmt.Errorf("scan slow_tools: %w", err)
		}
		status := "ok"
		if success == 0 {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "- %s: %dms [%s] at %s\n", name, durationMs, status, ts)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if count == 0 {
		return fmt.Sprintf("No tools exceeded %dms threshold.", threshold), nil
	}
	return b.String(), nil
}

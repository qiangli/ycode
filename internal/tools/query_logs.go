package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// QueryLogs is the exported entry point for log queries, usable by MCP server.
func QueryLogs(ctx context.Context, input json.RawMessage) (string, error) {
	return handleQueryLogs(ctx, input)
}

// RegisterQueryLogsHandler wires up the query_logs tool handler.
func RegisterQueryLogsHandler(r *Registry) {
	if spec, ok := r.Get("query_logs"); ok {
		spec.Handler = handleQueryLogs
	}
}

type queryLogsInput struct {
	QueryType string `json:"query_type"`
	Limit     int    `json:"limit"`
	Pattern   string `json:"pattern"`
	Level     string `json:"level"`
	SessionID string `json:"session_id"`
}

// logRecord represents a parsed conversation log entry.
type logRecord struct {
	Timestamp    string          `json:"timestamp"`
	SessionID    string          `json:"session_id"`
	TurnIndex    int             `json:"turn_index"`
	Model        string          `json:"model"`
	ResponseText string          `json:"response_text"`
	ToolCalls    []toolCallEntry `json:"tool_calls"`
	StopReason   string          `json:"stop_reason"`
	TokensIn     int             `json:"tokens_in"`
	TokensOut    int             `json:"tokens_out"`
	DurationMs   int64           `json:"duration_ms"`
	CostUSD      float64         `json:"estimated_cost_usd"`
	Success      bool            `json:"success"`
	Error        string          `json:"error"`
}

type toolCallEntry struct {
	Name       string `json:"name"`
	Success    bool   `json:"success"`
	Error      string `json:"error"`
	DurationMs int64  `json:"duration_ms"`
}

func handleQueryLogs(_ context.Context, input json.RawMessage) (string, error) {
	if otelDataDir == "" {
		return "", fmt.Errorf("OTEL data directory not configured")
	}

	var params queryLogsInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse query_logs input: %w", err)
	}
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	switch params.QueryType {
	case "recent_turns":
		return queryRecentTurns(params)
	case "turn_errors":
		return queryTurnErrors(params)
	case "search":
		return querySearchLogs(params)
	case "cost_summary":
		return queryCostSummary(params)
	default:
		return "", fmt.Errorf("unknown query_type: %q (valid: recent_turns, turn_errors, search, cost_summary)", params.QueryType)
	}
}

// loadLogs reads conversation JSONL log files.
func loadLogs(sessionFilter string, maxFiles int) ([]logRecord, error) {
	var paths []string

	// Shared logs dir.
	sharedDir := filepath.Join(otelDataDir, "logs")
	if entries, err := filepath.Glob(filepath.Join(sharedDir, "conversations-*.jsonl")); err == nil {
		paths = append(paths, entries...)
	}

	// Per-instance log dirs.
	instancesDir := filepath.Join(otelDataDir, "instances")
	if entries, err := os.ReadDir(instancesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if sessionFilter != "" && e.Name() != sessionFilter {
				continue
			}
			instLogs := filepath.Join(instancesDir, e.Name(), "logs")
			if files, err := filepath.Glob(filepath.Join(instLogs, "conversations-*.jsonl")); err == nil {
				paths = append(paths, files...)
			}
		}
	}

	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	if maxFiles > 0 && len(paths) > maxFiles {
		paths = paths[:maxFiles]
	}

	var records []logRecord
	for _, p := range paths {
		fileRecords, err := parseLogFile(p)
		if err != nil {
			continue
		}
		records = append(records, fileRecords...)
	}
	return records, nil
}

func parseLogFile(path string) ([]logRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []logRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 2*1024*1024), 2*1024*1024) // 2MB line limit
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r logRecord
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, scanner.Err()
}

func queryRecentTurns(p queryLogsInput) (string, error) {
	records, err := loadLogs(p.SessionID, 3)
	if err != nil {
		return "", err
	}
	// Most recent first.
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp > records[j].Timestamp
	})
	if len(records) > p.Limit {
		records = records[:p.Limit]
	}
	if len(records) == 0 {
		return "No conversation logs found.", nil
	}

	var b strings.Builder
	b.WriteString("Recent Turns:\n")
	b.WriteString("Turn | Model | Tokens(in/out) | Duration(ms) | Tools | Cost($) | Status\n")
	b.WriteString("---|---|---|---|---|---|---\n")
	for _, r := range records {
		status := "OK"
		if !r.Success {
			status = "ERROR"
		}
		fmt.Fprintf(&b, "%d | %s | %d/%d | %d | %d | %.4f | %s\n",
			r.TurnIndex, r.Model, r.TokensIn, r.TokensOut,
			r.DurationMs, len(r.ToolCalls), r.CostUSD, status)
	}
	return b.String(), nil
}

func queryTurnErrors(p queryLogsInput) (string, error) {
	records, err := loadLogs(p.SessionID, 5)
	if err != nil {
		return "", err
	}

	var errors []logRecord
	for _, r := range records {
		if !r.Success || r.Error != "" {
			errors = append(errors, r)
		}
	}
	sort.Slice(errors, func(i, j int) bool {
		return errors[i].Timestamp > errors[j].Timestamp
	})
	if len(errors) > p.Limit {
		errors = errors[:p.Limit]
	}
	if len(errors) == 0 {
		return "No turn errors found.", nil
	}

	var b strings.Builder
	b.WriteString("Turn Errors:\n")
	for _, r := range errors {
		fmt.Fprintf(&b, "- Turn %d at %s: %s (session: %s)\n",
			r.TurnIndex, r.Timestamp, r.Error, r.SessionID)
	}
	return b.String(), nil
}

func querySearchLogs(p queryLogsInput) (string, error) {
	if p.Pattern == "" {
		return "", fmt.Errorf("pattern is required for search query")
	}

	records, err := loadLogs(p.SessionID, 5)
	if err != nil {
		return "", err
	}

	pattern := strings.ToLower(p.Pattern)
	var matches []logRecord
	for _, r := range records {
		text := strings.ToLower(r.ResponseText + " " + r.Error)
		for _, tc := range r.ToolCalls {
			text += " " + strings.ToLower(tc.Name+" "+tc.Error)
		}
		if strings.Contains(text, pattern) {
			matches = append(matches, r)
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Timestamp > matches[j].Timestamp
	})
	if len(matches) > p.Limit {
		matches = matches[:p.Limit]
	}
	if len(matches) == 0 {
		return fmt.Sprintf("No logs matching %q found.", p.Pattern), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Logs matching %q (%d results):\n", p.Pattern, len(matches))
	for _, r := range matches {
		preview := r.ResponseText
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		fmt.Fprintf(&b, "- Turn %d at %s: %s\n", r.TurnIndex, r.Timestamp, preview)
	}
	return b.String(), nil
}

func queryCostSummary(p queryLogsInput) (string, error) {
	records, err := loadLogs(p.SessionID, 10)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "No conversation logs found.", nil
	}

	var totalCost float64
	var totalIn, totalOut int
	var totalDur int64
	var turnCount int
	sessions := make(map[string]bool)
	for _, r := range records {
		totalCost += r.CostUSD
		totalIn += r.TokensIn
		totalOut += r.TokensOut
		totalDur += r.DurationMs
		turnCount++
		sessions[r.SessionID] = true
	}

	var b strings.Builder
	b.WriteString("Cost Summary:\n")
	fmt.Fprintf(&b, "Sessions: %d | Turns: %d\n", len(sessions), turnCount)
	fmt.Fprintf(&b, "Tokens: %d in / %d out (total: %d)\n", totalIn, totalOut, totalIn+totalOut)
	fmt.Fprintf(&b, "Total cost: $%.4f\n", totalCost)
	fmt.Fprintf(&b, "Total LLM time: %dms\n", totalDur)
	if turnCount > 0 {
		fmt.Fprintf(&b, "Avg cost/turn: $%.4f | Avg duration: %dms\n",
			totalCost/float64(turnCount), totalDur/int64(turnCount))
	}
	return b.String(), nil
}

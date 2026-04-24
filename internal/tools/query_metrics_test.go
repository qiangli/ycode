package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/storage/sqlite"
)

// setupTestMetricsDB creates an in-memory SQLite store with migrations and test data.
func setupTestMetricsDB(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Insert a test session.
	_, err = s.Exec(ctx, `INSERT INTO sessions (id, title, model, token_input, token_output)
		VALUES ('test-session', 'Test Session', 'claude-4', 5000, 2000)`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// Insert tool usage data.
	inserts := []struct {
		tool    string
		dur     int
		success int
	}{
		{"bash", 150, 1},
		{"bash", 200, 1},
		{"bash", 8000, 0}, // slow + failed
		{"read_file", 50, 1},
		{"read_file", 30, 1},
		{"read_file", 40, 1},
		{"edit_file", 100, 1},
		{"edit_file", 120, 0},    // failed
		{"grep_search", 6000, 1}, // slow
	}
	for _, ins := range inserts {
		_, err := s.Exec(ctx, `INSERT INTO tool_usage (session_id, tool_name, duration_ms, success)
			VALUES (?, ?, ?, ?)`, "test-session", ins.tool, ins.dur, ins.success)
		if err != nil {
			t.Fatalf("insert tool_usage: %v", err)
		}
	}

	return s
}

func TestQueryMetrics_ToolStats(t *testing.T) {
	db := setupTestMetricsDB(t)
	metricsStore = db
	metricsSessionID = "test-session"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	input, _ := json.Marshal(queryMetricsInput{QueryType: "tool_stats"})
	result, err := handleQueryMetrics(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Tool Usage Stats:") {
		t.Errorf("expected header, got: %s", result)
	}
	if !strings.Contains(result, "bash") {
		t.Errorf("expected bash in results, got: %s", result)
	}
	if !strings.Contains(result, "read_file") {
		t.Errorf("expected read_file in results, got: %s", result)
	}
}

func TestQueryMetrics_ToolStatsAllSessions(t *testing.T) {
	db := setupTestMetricsDB(t)
	metricsStore = db
	metricsSessionID = "test-session"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	input, _ := json.Marshal(queryMetricsInput{QueryType: "tool_stats", SessionID: "all"})
	result, err := handleQueryMetrics(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "bash") {
		t.Errorf("expected bash in all-session results, got: %s", result)
	}
}

func TestQueryMetrics_RecentFailures(t *testing.T) {
	db := setupTestMetricsDB(t)
	metricsStore = db
	metricsSessionID = "test-session"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	input, _ := json.Marshal(queryMetricsInput{QueryType: "recent_failures"})
	result, err := handleQueryMetrics(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Recent Failures:") {
		t.Errorf("expected header, got: %s", result)
	}
	// Should contain bash (8000ms failure) and edit_file (120ms failure).
	if !strings.Contains(result, "bash") {
		t.Errorf("expected bash failure, got: %s", result)
	}
	if !strings.Contains(result, "edit_file") {
		t.Errorf("expected edit_file failure, got: %s", result)
	}
}

func TestQueryMetrics_SessionSummary(t *testing.T) {
	db := setupTestMetricsDB(t)
	metricsStore = db
	metricsSessionID = "test-session"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	input, _ := json.Marshal(queryMetricsInput{QueryType: "session_summary"})
	result, err := handleQueryMetrics(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Session: test-session") {
		t.Errorf("expected session ID, got: %s", result)
	}
	if !strings.Contains(result, "Title: Test Session") {
		t.Errorf("expected title, got: %s", result)
	}
	if !strings.Contains(result, "claude-4") {
		t.Errorf("expected model, got: %s", result)
	}
	if !strings.Contains(result, "5000 in / 2000 out") {
		t.Errorf("expected token counts, got: %s", result)
	}
	if !strings.Contains(result, "Tool Breakdown:") {
		t.Errorf("expected tool breakdown, got: %s", result)
	}
}

func TestQueryMetrics_SessionSummaryRejectsAll(t *testing.T) {
	db := setupTestMetricsDB(t)
	metricsStore = db
	metricsSessionID = "test-session"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	input, _ := json.Marshal(queryMetricsInput{QueryType: "session_summary", SessionID: "all"})
	_, err := handleQueryMetrics(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for session_summary with session_id=all")
	}
	if !strings.Contains(err.Error(), "requires a specific session") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestQueryMetrics_SlowTools(t *testing.T) {
	db := setupTestMetricsDB(t)
	metricsStore = db
	metricsSessionID = "test-session"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	input, _ := json.Marshal(queryMetricsInput{QueryType: "slow_tools", ThresholdMs: 5000})
	result, err := handleQueryMetrics(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Slow Tools (>5000ms):") {
		t.Errorf("expected header, got: %s", result)
	}
	// bash at 8000ms and grep_search at 6000ms should appear.
	if !strings.Contains(result, "bash") {
		t.Errorf("expected bash (8000ms), got: %s", result)
	}
	if !strings.Contains(result, "grep_search") {
		t.Errorf("expected grep_search (6000ms), got: %s", result)
	}
	// bash failure should show FAIL status.
	if !strings.Contains(result, "FAIL") {
		t.Errorf("expected FAIL marker, got: %s", result)
	}
}

func TestQueryMetrics_SlowToolsDefaultThreshold(t *testing.T) {
	db := setupTestMetricsDB(t)
	metricsStore = db
	metricsSessionID = "test-session"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	// No threshold_ms — should default to 5000.
	input, _ := json.Marshal(queryMetricsInput{QueryType: "slow_tools"})
	result, err := handleQueryMetrics(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, ">5000ms") {
		t.Errorf("expected default 5000ms threshold, got: %s", result)
	}
}

func TestQueryMetrics_NilStore(t *testing.T) {
	metricsStore = nil
	metricsSessionID = ""

	input, _ := json.Marshal(queryMetricsInput{QueryType: "tool_stats"})
	_, err := handleQueryMetrics(context.Background(), input)
	if err == nil {
		t.Fatal("expected error with nil store")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestQueryMetrics_InvalidQueryType(t *testing.T) {
	db := setupTestMetricsDB(t)
	metricsStore = db
	metricsSessionID = "test-session"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	input, _ := json.Marshal(queryMetricsInput{QueryType: "invalid"})
	_, err := handleQueryMetrics(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for invalid query_type")
	}
	if !strings.Contains(err.Error(), "unknown query_type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestQueryMetrics_EmptyResults(t *testing.T) {
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	metricsStore = s
	metricsSessionID = "nonexistent"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	input, _ := json.Marshal(queryMetricsInput{QueryType: "tool_stats"})
	result, err := handleQueryMetrics(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No tool usage data found." {
		t.Errorf("expected empty message, got: %s", result)
	}

	input, _ = json.Marshal(queryMetricsInput{QueryType: "recent_failures"})
	result, err = handleQueryMetrics(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No failures found." {
		t.Errorf("expected empty message, got: %s", result)
	}
}

func TestQueryMetrics_LimitClamping(t *testing.T) {
	db := setupTestMetricsDB(t)
	metricsStore = db
	metricsSessionID = "test-session"
	t.Cleanup(func() { metricsStore = nil; metricsSessionID = "" })

	// Limit=0 should default to 20; Limit=200 should cap to 100.
	// Both should succeed without error.
	for _, limit := range []int{0, 200} {
		input, _ := json.Marshal(queryMetricsInput{QueryType: "tool_stats", Limit: limit})
		_, err := handleQueryMetrics(context.Background(), input)
		if err != nil {
			t.Fatalf("unexpected error with limit=%d: %v", limit, err)
		}
	}
}

func TestQueryMetrics_MaskingExemption(t *testing.T) {
	if !session.ExemptFromMasking["query_metrics"] {
		t.Error("query_metrics should be exempt from observation masking")
	}
}

package routing

import (
	"context"
	"log/slog"
	"time"

	"github.com/qiangli/ycode/internal/storage"
)

// SQLStatsProvider queries the tool_usage table for observed performance data.
// Falls back to empty stats when the store is unavailable or has no data.
type SQLStatsProvider struct {
	Store     storage.SQLStore
	Logger    *slog.Logger
	LookBack  time.Duration // how far back to look for stats (default: 1 hour)
	MaxSample int           // max rows to consider (default: 50)
}

// NewSQLStatsProvider creates a stats provider backed by SQLite tool_usage data.
func NewSQLStatsProvider(store storage.SQLStore) *SQLStatsProvider {
	return &SQLStatsProvider{
		Store:     store,
		Logger:    slog.Default(),
		LookBack:  1 * time.Hour,
		MaxSample: 50,
	}
}

// Stats queries tool_usage for a model's observed latency and success rate.
func (s *SQLStatsProvider) Stats(ctx context.Context, model string, task TaskType) CandidateStats {
	if s.Store == nil {
		return CandidateStats{}
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	lookBack := s.LookBack
	if lookBack <= 0 {
		lookBack = 1 * time.Hour
	}
	maxSample := s.MaxSample
	if maxSample <= 0 {
		maxSample = 50
	}

	// Query average latency and success rate for this model+task_type
	// from recent tool_usage records.
	query := `SELECT
		COUNT(*) AS sample_count,
		COALESCE(AVG(duration_ms), 0) AS avg_ms,
		COALESCE(CAST(SUM(success) AS REAL) / NULLIF(COUNT(*), 0), 0) AS success_rate
	FROM (
		SELECT duration_ms, success
		FROM tool_usage
		WHERE model = ? AND task_type = ?
		  AND timestamp >= datetime('now', ?)
		ORDER BY timestamp DESC
		LIMIT ?
	)`

	lookBackStr := "-" + lookBack.String()
	row := s.Store.QueryRow(ctx, query, model, string(task), lookBackStr, maxSample)

	var sampleCount int
	var avgMs, successRate float64
	if err := row.Scan(&sampleCount, &avgMs, &successRate); err != nil {
		if s.Logger != nil {
			s.Logger.Debug("sql stats query failed", "model", model, "task", task, "error", err)
		}
		return CandidateStats{}
	}

	return CandidateStats{
		ObservedP50Ms: avgMs,
		SuccessRate:   successRate,
		SampleCount:   sampleCount,
	}
}

package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/qiangli/ycode/internal/storage"
)

// MetricsRecorder records tool invocation metrics to SQLite.
type MetricsRecorder struct {
	store     storage.SQLStore
	sessionID string
}

// NewMetricsRecorder creates a tool metrics recorder.
func NewMetricsRecorder(store storage.SQLStore, sessionID string) *MetricsRecorder {
	return &MetricsRecorder{store: store, sessionID: sessionID}
}

// Middleware returns a middleware that records tool execution metrics.
func (m *MetricsRecorder) Middleware(toolName string) Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			start := time.Now()
			result, err := next(ctx, input)
			duration := time.Since(start)

			success := 1
			if err != nil {
				success = 0
			}

			m.record(toolName, duration, success)
			return result, err
		}
	}
}

// ApplyToRegistry applies metrics middleware to all currently registered tools.
func (m *MetricsRecorder) ApplyToRegistry(registry *Registry) {
	for _, name := range registry.Names() {
		mw := m.Middleware(name)
		if err := registry.ApplyMiddleware(name, mw); err != nil {
			slog.Debug("metrics: apply middleware", "tool", name, "error", err)
		}
	}
}

func (m *MetricsRecorder) record(toolName string, duration time.Duration, success int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := m.store.Exec(ctx, `
		INSERT INTO tool_usage (session_id, tool_name, duration_ms, success, timestamp)
		VALUES (?, ?, ?, ?, datetime('now'))
	`, m.sessionID, toolName, duration.Milliseconds(), success)
	if err != nil {
		slog.Debug("metrics: record tool usage", "tool", toolName, "error", err)
	}
}

package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/qiangli/ycode/pkg/memex/store"
)

// MetricsRecorder records tool invocation metrics to SQLite. Writes are
// asynchronous: a single background writer drains a bounded channel, so a tool
// call never blocks on a DB INSERT (previously a synchronous, 5s-timeout write
// on the critical path that stalled every tool call when the DB was locked).
// One writer also collapses concurrent writers into one, removing the lock
// contention at the source. Metrics are best-effort observability; a dropped or
// lost row is acceptable.
type MetricsRecorder struct {
	store     store.SQLStore
	sessionID string
	ch        chan usageRow
}

type usageRow struct {
	tool     string
	duration time.Duration
	success  int
}

// NewMetricsRecorder creates a tool metrics recorder and starts its background
// writer, bound to ctx (stops on shutdown — no goroutine leak per session).
func NewMetricsRecorder(ctx context.Context, store store.SQLStore, sessionID string) *MetricsRecorder {
	m := &MetricsRecorder{store: store, sessionID: sessionID, ch: make(chan usageRow, 256)}
	go m.drain(ctx)
	return m
}

// drain serially writes queued metrics until ctx is cancelled.
func (m *MetricsRecorder) drain(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case row := <-m.ch:
			wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := m.store.Exec(wctx, `
				INSERT INTO tool_usage (session_id, tool_name, duration_ms, success, timestamp)
				VALUES (?, ?, ?, ?, datetime('now'))
			`, m.sessionID, row.tool, row.duration.Milliseconds(), row.success)
			cancel()
			if err != nil {
				slog.Debug("metrics: record tool usage", "tool", row.tool, "error", err)
			}
		}
	}
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

// record enqueues a metric non-blockingly. If the buffer is full it drops the
// row rather than block the tool call — best-effort observability, never latency.
func (m *MetricsRecorder) record(toolName string, duration time.Duration, success int) {
	select {
	case m.ch <- usageRow{tool: toolName, duration: duration, success: success}:
	default:
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

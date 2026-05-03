package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// WithRetry returns a middleware that retries failed tool calls with exponential backoff.
// Inspired by LangGraph's wrap_tool_call pattern for composable retry logic.
func WithRetry(maxAttempts int, baseDelay time.Duration) Middleware {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	if baseDelay <= 0 {
		baseDelay = 500 * time.Millisecond
	}
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			var lastErr error
			delay := baseDelay
			for attempt := 1; attempt <= maxAttempts; attempt++ {
				result, err := next(ctx, input)
				if err == nil {
					return result, nil
				}
				lastErr = err
				if attempt < maxAttempts {
					slog.Debug("tool retry", "attempt", attempt, "delay", delay, "error", err)
					select {
					case <-ctx.Done():
						return "", ctx.Err()
					case <-time.After(delay):
					}
					delay *= 2
				}
			}
			return "", fmt.Errorf("after %d attempts: %w", maxAttempts, lastErr)
		}
	}
}

// WithTimeout returns a middleware that enforces a per-call timeout.
func WithTimeout(timeout time.Duration) Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return next(ctx, input)
		}
	}
}

// ToolEventType identifies the kind of event emitted during tool execution.
type ToolEventType string

const (
	ToolEventStarted  ToolEventType = "tool-started"
	ToolEventDelta    ToolEventType = "tool-output-delta"
	ToolEventFinished ToolEventType = "tool-finished"
	ToolEventError    ToolEventType = "tool-error"
)

// ToolEvent is emitted during tool execution for streaming visibility.
type ToolEvent struct {
	Type    ToolEventType `json:"type"`
	Tool    string        `json:"tool"`
	CallID  string        `json:"call_id,omitempty"`
	Payload string        `json:"payload,omitempty"`
}

// ToolEventWriter receives streaming events from a tool execution.
// Tools that support streaming can pull the writer from context and
// emit deltas as they produce output, inspired by LangGraph's
// StreamToolCallHandler with per-call context-var binding.
type ToolEventWriter interface {
	WriteEvent(event ToolEvent)
}

type toolEventWriterKey struct{}

// ContextWithToolEventWriter returns a context with an attached ToolEventWriter.
func ContextWithToolEventWriter(ctx context.Context, w ToolEventWriter) context.Context {
	return context.WithValue(ctx, toolEventWriterKey{}, w)
}

// ToolEventWriterFromContext retrieves the ToolEventWriter from context, or nil.
func ToolEventWriterFromContext(ctx context.Context) ToolEventWriter {
	w, _ := ctx.Value(toolEventWriterKey{}).(ToolEventWriter)
	return w
}

// WithEventWriter returns a middleware that injects a ToolEventWriter into context
// and emits started/finished/error events around each tool call.
func WithEventWriter(w ToolEventWriter) Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, input json.RawMessage) (string, error) {
			// Inject writer so tools can emit deltas.
			ctx = ContextWithToolEventWriter(ctx, w)

			// Extract tool name from context if available (set by registry).
			toolName := toolNameFromContext(ctx)

			w.WriteEvent(ToolEvent{
				Type: ToolEventStarted,
				Tool: toolName,
			})

			result, err := next(ctx, input)

			if err != nil {
				w.WriteEvent(ToolEvent{
					Type:    ToolEventError,
					Tool:    toolName,
					Payload: err.Error(),
				})
			} else {
				w.WriteEvent(ToolEvent{
					Type: ToolEventFinished,
					Tool: toolName,
				})
			}

			return result, err
		}
	}
}

type toolNameKey struct{}

// ContextWithToolName returns a context with the tool name attached.
func ContextWithToolName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, toolNameKey{}, name)
}

func toolNameFromContext(ctx context.Context) string {
	name, _ := ctx.Value(toolNameKey{}).(string)
	return name
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// CompactFunc is a callback that triggers context compaction.
// It receives a context and returns a summary string and error.
type CompactFunc func(ctx context.Context) (summary string, compactedCount int, preservedCount int, err error)

// RegisterCompactContextHandler registers the compact_context tool handler.
// The compactFn callback is provided by the app layer and triggers actual compaction.
func RegisterCompactContextHandler(r *Registry, compactFn CompactFunc) {
	spec, ok := r.Get("compact_context")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Reason string `json:"reason"`
		}
		if input != nil {
			_ = json.Unmarshal(input, &params)
		}

		summary, compacted, preserved, err := compactFn(ctx)
		if err != nil {
			return "", fmt.Errorf("compact_context: %w", err)
		}

		result := fmt.Sprintf("Context compacted successfully.\nCompacted: %d messages\nPreserved: %d messages\nSummary preview: %s",
			compacted, preserved, truncateStr(summary, 300))

		if params.Reason != "" {
			result = fmt.Sprintf("Reason: %s\n%s", params.Reason, result)
		}

		return result, nil
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

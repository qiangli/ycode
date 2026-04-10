package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RegisterSleepHandler registers the Sleep tool handler.
func RegisterSleepHandler(r *Registry) {
	spec, ok := r.Get("Sleep")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			DurationMs int `json:"duration_ms"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse Sleep input: %w", err)
		}
		if params.DurationMs <= 0 {
			return "", fmt.Errorf("duration_ms must be positive")
		}
		if params.DurationMs > 300000 { // 5 minutes max
			params.DurationMs = 300000
		}

		d := time.Duration(params.DurationMs) * time.Millisecond
		select {
		case <-time.After(d):
			return fmt.Sprintf("Slept for %v", d), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

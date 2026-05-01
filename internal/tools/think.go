package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// RegisterThinkHandler registers the Think tool handler.
func RegisterThinkHandler(r *Registry) {
	spec, ok := r.Get("Think")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Thought string `json:"thought"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse Think input: %w", err)
		}
		if params.Thought == "" {
			return "", fmt.Errorf("thought is required")
		}
		return fmt.Sprintf("Thought recorded: %s", params.Thought), nil
	}
}

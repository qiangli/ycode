package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// RegisterCompletionHandler registers the AttemptCompletion tool handler.
func RegisterCompletionHandler(r *Registry) {
	spec, ok := r.Get("AttemptCompletion")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Result  string `json:"result"`
			Command string `json:"command,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse AttemptCompletion input: %w", err)
		}
		if params.Result == "" {
			return "", fmt.Errorf("result is required")
		}

		output := fmt.Sprintf("Task completed: %s", params.Result)
		if params.Command != "" {
			output += fmt.Sprintf("\nVerification command: %s", params.Command)
		}
		return output, nil
	}
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// handoffSignal is the JSON structure returned by the Handoff tool.
// The orchestrator in the swarm package detects this marker.
type handoffSignal struct {
	Handoff bool           `json:"__handoff__"`
	Result  handoffResult  `json:"result"`
}

type handoffResult struct {
	TargetAgent string            `json:"target_agent"`
	ContextVars map[string]string `json:"context_vars,omitempty"`
	Message     string            `json:"message,omitempty"`
}

// RegisterHandoffHandler registers the Handoff tool handler.
// The Handoff tool allows an agent to transfer control to another agent,
// passing context variables and a message.
func RegisterHandoffHandler(r *Registry) {
	spec, ok := r.Get("Handoff")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			TargetAgent string            `json:"target_agent"`
			ContextVars map[string]string `json:"context_vars,omitempty"`
			Message     string            `json:"message,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse Handoff input: %w", err)
		}
		if params.TargetAgent == "" {
			return "", fmt.Errorf("target_agent is required")
		}

		sig := handoffSignal{
			Handoff: true,
			Result: handoffResult{
				TargetAgent: params.TargetAgent,
				ContextVars: params.ContextVars,
				Message:     params.Message,
			},
		}
		data, err := json.Marshal(sig)
		if err != nil {
			return "", fmt.Errorf("marshal handoff signal: %w", err)
		}
		return string(data), nil
	}
}

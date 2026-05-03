package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// handoffSignal is the JSON structure returned by the Handoff tool.
// The orchestrator in the swarm package detects this marker.
type handoffSignal struct {
	Handoff bool          `json:"__handoff__"`
	Result  handoffResult `json:"result"`
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
	RegisterHandoffHandlerWithAgents(r, nil)
}

// RegisterHandoffHandlerWithAgents registers the Handoff tool handler with
// a list of valid agent names. When validAgents is non-nil and non-empty,
// the handler validates the target_agent against this list, preventing
// hallucinated agent names. Inspired by ADK-Python's TransferToAgentTool
// which uses JSON schema enum constraints.
func RegisterHandoffHandlerWithAgents(r *Registry, validAgents []string) {
	spec, ok := r.Get("Handoff")
	if !ok {
		return
	}

	// Build lookup set for validation.
	agentSet := make(map[string]bool, len(validAgents))
	for _, name := range validAgents {
		agentSet[name] = true
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

		// Validate target against known agents if a list was provided.
		if len(agentSet) > 0 && !agentSet[params.TargetAgent] {
			names := make([]string, 0, len(agentSet))
			for name := range agentSet {
				names = append(names, name)
			}
			return "", fmt.Errorf("unknown agent %q — valid agents: %v", params.TargetAgent, names)
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

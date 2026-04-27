// Package swarm provides multi-agent orchestration with handoff, flow execution,
// context variable passing, and inter-agent messaging.
package swarm

import (
	"encoding/json"
	"fmt"
)

// handoffMarker is the sentinel key in tool results that signals a handoff.
const handoffMarker = "__handoff__"

// HandoffResult describes a control transfer from one agent to another.
type HandoffResult struct {
	TargetAgent string            `json:"target_agent"`
	ContextVars map[string]string `json:"context_vars,omitempty"`
	Message     string            `json:"message,omitempty"`
}

// HandoffSignal wraps a HandoffResult with the marker for detection.
type HandoffSignal struct {
	Handoff bool          `json:"__handoff__"`
	Result  HandoffResult `json:"result"`
}

// NewHandoffSignal creates a handoff signal JSON for the given target agent.
func NewHandoffSignal(target string, contextVars map[string]string, message string) (string, error) {
	sig := HandoffSignal{
		Handoff: true,
		Result: HandoffResult{
			TargetAgent: target,
			ContextVars: contextVars,
			Message:     message,
		},
	}
	data, err := json.Marshal(sig)
	if err != nil {
		return "", fmt.Errorf("marshal handoff signal: %w", err)
	}
	return string(data), nil
}

// DetectHandoff checks if a tool result contains a handoff signal.
// Returns the HandoffResult and true if a handoff was detected.
func DetectHandoff(toolResult string) (*HandoffResult, bool) {
	var sig HandoffSignal
	if err := json.Unmarshal([]byte(toolResult), &sig); err != nil {
		return nil, false
	}
	if !sig.Handoff {
		return nil, false
	}
	if sig.Result.TargetAgent == "" {
		return nil, false
	}
	return &sig.Result, true
}

// DetectHandoffInResults checks multiple tool results for a handoff signal.
// Returns the first handoff found, or nil if none.
func DetectHandoffInResults(results []string) (*HandoffResult, bool) {
	for _, r := range results {
		if hr, ok := DetectHandoff(r); ok {
			return hr, true
		}
	}
	return nil, false
}

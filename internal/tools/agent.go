package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// AgentType identifies the subagent type.
type AgentType string

const (
	AgentExplore        AgentType = "Explore"
	AgentPlan           AgentType = "Plan"
	AgentVerification   AgentType = "Verification"
	AgentGeneralPurpose AgentType = "general-purpose"
	AgentGuide          AgentType = "claw-guide"
	AgentStatusLine     AgentType = "statusline-setup"
)

// AllowedToolsForAgent returns the tool allowlist for each agent type.
func AllowedToolsForAgent(agentType AgentType) []string {
	readOnly := []string{"read_file", "glob_search", "grep_search", "WebFetch", "WebSearch", "ToolSearch", "Skill"}

	switch agentType {
	case AgentExplore:
		return readOnly
	case AgentPlan:
		return append(readOnly, "TodoWrite", "SendUserMessage")
	case AgentVerification:
		return append(readOnly, "TodoWrite", "SendUserMessage", "bash", "write_file", "edit_file", "REPL", "PowerShell")
	case AgentGeneralPurpose:
		return []string{
			"bash", "read_file", "write_file", "edit_file", "glob_search", "grep_search",
			"WebFetch", "WebSearch", "TodoWrite", "Skill", "AskUserQuestion",
			"SendUserMessage", "ToolSearch", "NotebookEdit", "Sleep",
		}
	case AgentGuide:
		return append(readOnly, "SendUserMessage")
	case AgentStatusLine:
		return []string{"read_file", "edit_file", "Config"}
	default:
		return readOnly
	}
}

// AgentManifest records a spawned agent and its configuration.
type AgentManifest struct {
	ID          string    `json:"id"`
	Type        AgentType `json:"type"`
	Description string    `json:"description"`
	Prompt      string    `json:"prompt"`
	Depth       int       `json:"depth"`
}

// RegisterAgentHandler registers the Agent tool handler.
func RegisterAgentHandler(r *Registry, spawner func(ctx context.Context, manifest *AgentManifest) (string, error)) {
	spec, ok := r.Get("Agent")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Description  string `json:"description"`
			Prompt       string `json:"prompt"`
			SubagentType string `json:"subagent_type,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse Agent input: %w", err)
		}

		agentType := AgentGeneralPurpose
		if params.SubagentType != "" {
			agentType = AgentType(params.SubagentType)
		}

		// Validate agent type.
		validTypes := []AgentType{AgentExplore, AgentPlan, AgentVerification, AgentGeneralPurpose, AgentGuide, AgentStatusLine}
		valid := false
		for _, t := range validTypes {
			if agentType == t {
				valid = true
				break
			}
		}
		if !valid {
			return "", fmt.Errorf("invalid agent type: %s (valid: %s)",
				agentType, strings.Join(agentTypeStrings(validTypes), ", "))
		}

		manifest := &AgentManifest{
			Type:        agentType,
			Description: params.Description,
			Prompt:      params.Prompt,
		}

		if spawner != nil {
			return spawner(ctx, manifest)
		}

		return fmt.Sprintf("Agent spawned (type: %s, desc: %s)", agentType, params.Description), nil
	}
}

func agentTypeStrings(types []AgentType) []string {
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = string(t)
	}
	return result
}

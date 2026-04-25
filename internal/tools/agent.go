package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/task"
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

// AgentMode controls the runtime behavior: system prompt, tool access, and constraints.
type AgentMode string

const (
	ModeBuild   AgentMode = "build"   // full tool access, default mode
	ModePlan    AgentMode = "plan"    // read-only, structured planning workflow
	ModeExplore AgentMode = "explore" // read-only subagent for codebase search
)

// AllowedToolsForMode returns the tool allowlist for a given agent mode.
// Returns nil for build mode (all tools allowed).
func AllowedToolsForMode(mode AgentMode) []string {
	switch mode {
	case ModeExplore:
		return []string{
			"bash", "read_file", "glob_search", "grep_search",
			"WebFetch", "WebSearch", "ToolSearch",
		}
	case ModePlan:
		return []string{
			"bash", "read_file", "glob_search", "grep_search",
			"WebFetch", "WebSearch", "ToolSearch", "Skill",
			"Agent", "AskUserQuestion",
			"EnterPlanMode", "ExitPlanMode",
		}
	case ModeBuild:
		return nil // all tools
	default:
		return nil
	}
}

// AgentTypeToMode maps a subagent type to its runtime mode.
func AgentTypeToMode(t AgentType) AgentMode {
	switch t {
	case AgentExplore:
		return ModeExplore
	case AgentPlan:
		return ModePlan
	default:
		return ModeBuild
	}
}

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
	ID              string    `json:"id"`
	Type            AgentType `json:"type"`
	Description     string    `json:"description"`
	Prompt          string    `json:"prompt"`
	Depth           int       `json:"depth"`
	RunInBackground bool      `json:"run_in_background,omitempty"`
}

// RegisterAgentHandler registers the Agent tool handler.
// parentMode controls subagent constraints: when the parent is in plan mode,
// all subagents are forced to explore type.
// taskRegistry is optional; when provided, background agents are tracked as tasks.
func RegisterAgentHandler(r *Registry, parentMode func() AgentMode, spawner func(ctx context.Context, manifest *AgentManifest) (string, error), taskRegistry *task.Registry) {
	spec, ok := r.Get("Agent")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Description     string `json:"description"`
			Prompt          string `json:"prompt"`
			SubagentType    string `json:"subagent_type,omitempty"`
			RunInBackground bool   `json:"run_in_background,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse Agent input: %w", err)
		}

		agentType := AgentGeneralPurpose
		if params.SubagentType != "" {
			agentType = AgentType(params.SubagentType)
		}

		// In plan mode, force all subagents to explore type.
		if parentMode != nil && parentMode() == ModePlan {
			agentType = AgentExplore
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
			Type:            agentType,
			Description:     params.Description,
			Prompt:          params.Prompt,
			RunInBackground: params.RunInBackground,
		}

		if spawner == nil {
			return fmt.Sprintf("Agent spawned (type: %s, desc: %s)", agentType, params.Description), nil
		}

		// Background execution: launch as a tracked task and return immediately.
		if manifest.RunInBackground && taskRegistry != nil {
			t := taskRegistry.Create(fmt.Sprintf("agent:%s — %s", agentType, params.Description),
				func(taskCtx context.Context) (string, error) {
					return spawner(taskCtx, manifest)
				},
			)
			return fmt.Sprintf("Agent started in background (task_id: %s, type: %s, desc: %s)", t.ID, agentType, params.Description), nil
		}

		return spawner(ctx, manifest)
	}
}

func agentTypeStrings(types []AgentType) []string {
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = string(t)
	}
	return result
}

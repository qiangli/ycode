package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

// SwarmRunner is the function type for running a named agent with a prompt.
// Injected by the caller to avoid import cycles with agentdef/swarm packages.
type SwarmRunner func(ctx context.Context, agentName, prompt string) (string, error)

// RegisterSwarmHandler registers the swarm_run tool that delegates work
// to named agents or flows defined in agents/*.yaml.
func RegisterSwarmHandler(r *Registry, runner SwarmRunner) {
	if runner == nil {
		return
	}

	spec, ok := r.Get("swarm_run")
	if !ok {
		return
	}

	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Agent  string `json:"agent"`
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse swarm_run input: %w", err)
		}

		if params.Agent == "" {
			return "", fmt.Errorf("agent name is required")
		}
		if params.Prompt == "" {
			return "", fmt.Errorf("prompt is required")
		}

		return runner(ctx, params.Agent, params.Prompt)
	}
}

// swarmRunSpec defines the swarm_run tool specification.
func swarmRunSpec() *ToolSpec {
	return &ToolSpec{
		Name: "swarm_run",
		Description: `Run a named agent or multi-agent flow from agent definitions.
Delegates work to custom agents defined in agents/*.yaml files with handoff support.
Use this when a task requires specialized agent behavior defined in the project's agent configurations.`,
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"agent": {
					"type": "string",
					"description": "Name of the agent or flow to run (must match an agents/*.yaml definition)"
				},
				"prompt": {
					"type": "string",
					"description": "The task or prompt to send to the agent"
				}
			},
			"required": ["agent", "prompt"]
		}`),
		RequiredMode:    permission.DangerFullAccess,
		Source:          SourceBuiltin,
		AlwaysAvailable: false,
		Category:        CategoryAgent,
	}
}

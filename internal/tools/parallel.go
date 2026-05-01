package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qiangli/ycode/internal/runtime/agentdef"
)

// maxParallelAgents is the maximum number of agents that can run in parallel.
const maxParallelAgents = 10

// defaultParallelTimeout is the default timeout for parallel agent execution (5 min).
const defaultParallelTimeout = 5 * time.Minute

// RegisterParallelAgentsHandler registers the ParallelAgents tool handler.
// The spawner function is the same closure used by the Agent tool to create
// subagent loops; it blocks until the agent completes.
func RegisterParallelAgentsHandler(
	r *Registry,
	spawner func(ctx context.Context, manifest *AgentManifest) (string, error),
) {
	spec, ok := r.Get("ParallelAgents")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Agents []struct {
				Description string `json:"description"`
				Prompt      string `json:"prompt"`
				AgentType   string `json:"agent_type,omitempty"`
				Model       string `json:"model,omitempty"`
			} `json:"agents"`
			Timeout int `json:"timeout,omitempty"` // milliseconds
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse ParallelAgents input: %w", err)
		}

		if len(params.Agents) == 0 {
			return "", fmt.Errorf("at least one agent is required")
		}
		if len(params.Agents) > maxParallelAgents {
			return "", fmt.Errorf("too many agents: %d (max %d)", len(params.Agents), maxParallelAgents)
		}

		for i, a := range params.Agents {
			if a.Description == "" {
				return "", fmt.Errorf("agent[%d]: description is required", i)
			}
			if a.Prompt == "" {
				return "", fmt.Errorf("agent[%d]: prompt is required", i)
			}
		}

		// Apply timeout.
		timeout := defaultParallelTimeout
		if params.Timeout > 0 {
			timeout = time.Duration(params.Timeout) * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Build an Action closure for each agent entry.
		descriptions := make([]string, len(params.Agents))
		actions := make([]agentdef.Action, len(params.Agents))

		for i, a := range params.Agents {
			descriptions[i] = a.Description

			agentType := AgentType(a.AgentType)
			if agentType == "" {
				agentType = AgentGeneralPurpose
			}

			manifest := &AgentManifest{
				ID:          uuid.New().String(),
				Type:        agentType,
				Description: a.Description,
				Prompt:      a.Prompt,
				Model:       a.Model,
			}

			actions[i] = func(ctx context.Context, _ string) (string, error) {
				return spawner(ctx, manifest)
			}
		}

		// Use FlowParallel to run all agents concurrently.
		executor := agentdef.NewFlowExecutor(agentdef.FlowParallel, actions)
		combined, err := executor.Run(ctx, "")
		if err != nil {
			return "", fmt.Errorf("parallel execution failed: %w", err)
		}

		// Post-process: add description headers to each result section.
		// FlowParallel joins results with "\n---\n".
		sections := strings.Split(combined, "\n---\n")
		var b strings.Builder
		for i, section := range sections {
			desc := "agent"
			if i < len(descriptions) {
				desc = descriptions[i]
			}
			fmt.Fprintf(&b, "=== Agent %d: %s ===\n%s\n\n", i+1, desc, strings.TrimSpace(section))
		}
		return b.String(), nil
	}
}

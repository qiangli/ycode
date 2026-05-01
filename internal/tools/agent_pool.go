package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/agentpool"
	"github.com/qiangli/ycode/internal/runtime/task"
)

// RegisterAgentPoolHandlers registers agent orchestration tools:
// AgentList, AgentWait, AgentClose.
func RegisterAgentPoolHandlers(r *Registry, pool *agentpool.Pool, taskReg *task.Registry) {
	registerAgentListHandler(r, pool)
	registerAgentWaitHandler(r, taskReg)
	registerAgentCloseHandler(r, taskReg)
}

func registerAgentListHandler(r *Registry, pool *agentpool.Pool) {
	spec, ok := r.Get("AgentList")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ActiveOnly bool `json:"active_only"`
		}
		_ = json.Unmarshal(input, &params)

		var agents []agentpool.AgentInfo
		if params.ActiveOnly {
			agents = pool.Active()
		} else {
			agents = pool.All()
		}

		if len(agents) == 0 {
			return "No agents found.", nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Agents (%d):\n\n", len(agents))
		fmt.Fprintf(&b, "| ID | Type | Status | Tools | Duration | Description |\n")
		fmt.Fprintf(&b, "|----|------|--------|-------|----------|-------------|\n")
		for _, a := range agents {
			dur := a.Duration().Truncate(time.Second)
			fmt.Fprintf(&b, "| %s | %s | %s | %d | %s | %s |\n",
				a.ID[:8], a.Type, a.Status, a.ToolUses, dur, a.Description)
		}
		return b.String(), nil
	}
}

func registerAgentWaitHandler(r *Registry, taskReg *task.Registry) {
	spec, ok := r.Get("AgentWait")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			TaskID  string `json:"task_id"`
			Timeout int    `json:"timeout"` // milliseconds, default 60000
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse AgentWait input: %w", err)
		}
		if params.TaskID == "" {
			return "", fmt.Errorf("task_id is required")
		}
		if params.Timeout <= 0 {
			params.Timeout = 60000
		}

		deadline := time.After(time.Duration(params.Timeout) * time.Millisecond)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-deadline:
				return fmt.Sprintf("Timeout waiting for task %s after %dms.", params.TaskID, params.Timeout), nil
			case <-ticker.C:
				t, ok := taskReg.Get(params.TaskID)
				if !ok {
					return "", fmt.Errorf("task %q not found", params.TaskID)
				}
				switch t.Status {
				case "completed":
					return fmt.Sprintf("Task %s completed. Output: %s", params.TaskID, t.Output), nil
				case "failed":
					return fmt.Sprintf("Task %s failed: %s", params.TaskID, t.Error), nil
				case "stopped":
					return fmt.Sprintf("Task %s was stopped.", params.TaskID), nil
				}
				// Still running — continue polling.
			}
		}
	}
}

func registerAgentCloseHandler(r *Registry, taskReg *task.Registry) {
	spec, ok := r.Get("AgentClose")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse AgentClose input: %w", err)
		}
		if params.TaskID == "" {
			return "", fmt.Errorf("task_id is required")
		}

		t, ok := taskReg.Get(params.TaskID)
		if !ok {
			return "", fmt.Errorf("task %q not found", params.TaskID)
		}

		if t.Status == "completed" || t.Status == "failed" || t.Status == "stopped" {
			return fmt.Sprintf("Task %s already %s.", params.TaskID, t.Status), nil
		}

		if err := taskReg.Stop(params.TaskID); err != nil {
			return "", fmt.Errorf("stop task: %w", err)
		}
		return fmt.Sprintf("Agent task %s closed.", params.TaskID), nil
	}
}

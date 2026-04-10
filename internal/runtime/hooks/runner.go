package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Event describes a hook event.
type Event struct {
	Name     string          `json:"name"`
	ToolName string          `json:"tool_name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Output   string          `json:"output,omitempty"`
}

// HookConfig describes a configured hook.
type HookConfig struct {
	Event   string `json:"event"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // milliseconds
}

// Runner executes configured hooks.
type Runner struct {
	hooks []HookConfig
}

// NewRunner creates a new hook runner.
func NewRunner(hooks []HookConfig) *Runner {
	return &Runner{hooks: hooks}
}

// Run fires all hooks matching the event.
func (r *Runner) Run(ctx context.Context, event *Event) error {
	for _, h := range r.hooks {
		if h.Event != event.Name {
			continue
		}

		timeout := 10 * time.Second
		if h.Timeout > 0 {
			timeout = time.Duration(h.Timeout) * time.Millisecond
		}

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "bash", "-c", h.Command)
		eventJSON, _ := json.Marshal(event)
		cmd.Stdin = nil
		cmd.Env = append(cmd.Environ(), fmt.Sprintf("YCODE_HOOK_EVENT=%s", string(eventJSON)))

		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("hook %q failed: %w\noutput: %s", h.Command, err, string(output))
		}
	}
	return nil
}

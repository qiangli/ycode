package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
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

		// Parse the hook command.
		prog, err := syntax.NewParser().Parse(strings.NewReader(h.Command), "")
		if err != nil {
			return fmt.Errorf("hook %q parse error: %w", h.Command, err)
		}

		// Build environment with hook event data.
		eventJSON, _ := json.Marshal(event)
		env := append(os.Environ(), fmt.Sprintf("YCODE_HOOK_EVENT=%s", string(eventJSON)))

		var output bytes.Buffer
		stdin, _ := os.Open(os.DevNull)
		defer stdin.Close()

		runner, err := interp.New(
			interp.StdIO(stdin, &output, &output),
			interp.Env(expand.ListEnviron(env...)),
		)
		if err != nil {
			return fmt.Errorf("hook %q interpreter error: %w", h.Command, err)
		}

		if err := runner.Run(ctx, prog); err != nil {
			return fmt.Errorf("hook %q failed: %w\noutput: %s", h.Command, err, output.String())
		}
	}
	return nil
}

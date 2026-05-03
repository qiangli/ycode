package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// ShellHookHandler runs a shell command and interprets the result.
// Protocol: event JSON on stdin, exit code 0=continue/2=block,
// JSON HookResponse on stdout (optional).
type ShellHookHandler struct {
	Command string
	Timeout time.Duration
}

// NewShellHookHandler creates a shell hook handler.
func NewShellHookHandler(command string, timeoutMs int) *ShellHookHandler {
	timeout := 30 * time.Second
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	return &ShellHookHandler{
		Command: command,
		Timeout: timeout,
	}
}

// Execute runs the shell command with the event payload on stdin.
func (h *ShellHookHandler) Execute(ctx context.Context, event string, payload *Event) (*HookResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", h.Command)
	cmd.Stdin = bytes.NewReader(payloadJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("hook command failed: %w (stderr: %s)", err, stderr.String())
		}
	}

	resp := &HookResponse{Action: ActionContinue}
	if stdout.Len() > 0 {
		if jsonErr := json.Unmarshal(stdout.Bytes(), resp); jsonErr != nil {
			resp.Message = stdout.String()
		}
	}

	if exitCode == 2 {
		resp.Action = ActionBlock
		if resp.Message == "" {
			resp.Message = fmt.Sprintf("Hook blocked execution (exit code 2)")
		}
	}

	return resp, nil
}

// GoHookHandler wraps a Go function as a hook handler.
type GoHookHandler struct {
	Fn func(ctx context.Context, event string, payload *Event) (*HookResponse, error)
}

// Execute calls the wrapped Go function.
func (h *GoHookHandler) Execute(ctx context.Context, event string, payload *Event) (*HookResponse, error) {
	return h.Fn(ctx, event, payload)
}

// BuildRegistrations creates hook registrations from config entries.
func BuildRegistrations(configs []HookConfig) []Registration {
	var regs []Registration
	for _, cfg := range configs {
		handler := NewShellHookHandler(cfg.Command, cfg.Timeout)
		regs = append(regs, Registration{
			Handler: handler,
		})
	}
	return regs
}

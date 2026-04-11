package bash

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// DefaultTimeout for bash commands.
	DefaultTimeout = 120 * time.Second
	// MaxTimeout is the maximum allowed timeout.
	MaxTimeout = 10 * time.Minute
	// MaxOutputSize limits command output.
	MaxOutputSize = 512 * 1024 // 512 KB
)

// ExecParams configures bash execution.
type ExecParams struct {
	Command     string `json:"command"`
	Timeout     int    `json:"timeout,omitempty"` // milliseconds
	Background  bool   `json:"run_in_background,omitempty"`
	Description string `json:"description,omitempty"`
	WorkDir     string `json:"-"`
}

// ExecResult holds the result of a bash execution.
type ExecResult struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
}

// Execute runs a bash command and returns the result.
func Execute(ctx context.Context, params ExecParams) (*ExecResult, error) {
	timeout := DefaultTimeout
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Millisecond
		if timeout > MaxTimeout {
			timeout = MaxTimeout
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", params.Command)
	if params.WorkDir != "" {
		cmd.Dir = params.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &ExecResult{
		Stdout: truncateOutput(stdout.String()),
		Stderr: truncateOutput(stderr.String()),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			return result, fmt.Errorf("command timed out after %v", timeout)
		} else {
			return result, fmt.Errorf("execute command: %w", err)
		}
	}

	return result, nil
}

// truncateOutput limits output to MaxOutputSize.
func truncateOutput(s string) string {
	if len(s) <= MaxOutputSize {
		return strings.TrimRight(s, "\n")
	}
	return s[:MaxOutputSize] + "\n... (output truncated)"
}

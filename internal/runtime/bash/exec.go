package bash

import (
	"bytes"
	"context"
	"fmt"
	"os"
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

// ErrNeedsTTY is returned when a command requires interactive terminal access.
// The user should run these commands via "!! <command>" in the TUI.
var ErrNeedsTTY = fmt.Errorf("command requires interactive terminal access")

// NeedsTTY returns true if a command is likely to prompt for interactive
// input (password, confirmation, etc.) and needs full terminal access.
func NeedsTTY(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	base := fields[0]

	// Commands that commonly prompt for passwords or interactive input.
	switch base {
	case "ssh", "scp", "sftp":
		return true
	case "sudo", "su", "passwd":
		return true
	case "gcloud":
		// gcloud auth login, gcloud init
		if len(fields) > 1 && (fields[1] == "auth" || fields[1] == "init") {
			return true
		}
	case "az":
		// az login
		if len(fields) > 1 && fields[1] == "login" {
			return true
		}
	case "aws":
		// aws sso login, aws configure
		if len(fields) > 1 && (fields[1] == "sso" || fields[1] == "configure") {
			return true
		}
	case "docker":
		if len(fields) > 1 && fields[1] == "login" {
			return true
		}
	case "gh":
		if len(fields) > 1 && fields[1] == "auth" {
			return true
		}
	case "npm":
		if len(fields) > 1 && fields[1] == "login" {
			return true
		}
	case "mysql", "psql", "mongo", "mongosh", "redis-cli":
		return true
	case "ftp", "telnet":
		return true
	}

	// Pipe to interactive pager.
	if strings.Contains(command, "| less") || strings.Contains(command, "| more") ||
		strings.Contains(command, "| vi") || strings.Contains(command, "| vim") ||
		strings.Contains(command, "| nano") || strings.Contains(command, "| emacs") {
		return true
	}

	// Editors launched directly.
	switch base {
	case "vi", "vim", "nvim", "nano", "emacs", "pico", "less", "more":
		return true
	}

	return false
}

// Execute runs a bash command and returns the result.
func Execute(ctx context.Context, params ExecParams) (*ExecResult, error) {
	// Reject commands that need interactive terminal access — they would
	// hang waiting for stdin that can never arrive through piped execution.
	if NeedsTTY(params.Command) {
		return nil, fmt.Errorf("%w: %q requires user interaction (password, confirmation, etc.). "+
			"The user should run this command directly in their terminal using: !! %s",
			ErrNeedsTTY, params.Command, params.Command)
	}

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
	// Explicitly prevent stdin reads — commands that need interactive input
	// are rejected above; this ensures any missed cases get EOF instead of hanging.
	cmd.Stdin, _ = os.Open(os.DevNull)

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

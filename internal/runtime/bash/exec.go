package bash

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/qiangli/ycode/internal/runtime/bash/shellparse"
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
	Command     string       `json:"command"`
	Timeout     int          `json:"timeout,omitempty"` // milliseconds
	Background  bool         `json:"run_in_background,omitempty"`
	Description string       `json:"description,omitempty"`
	Stdin       string       `json:"stdin,omitempty"` // content to pipe to stdin
	WorkDir     string       `json:"-"`
	TTYExec     TTYExecutor  `json:"-"` // optional: delegate interactive commands here
	Jobs        *JobRegistry `json:"-"` // optional: registry for background job tracking
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
	// Try AST-based detection for accurate command name extraction.
	nodes, err := shellparse.Parse(command)
	if err == nil && len(nodes) > 0 {
		return needsTTYFromNodes(nodes, command)
	}

	// Fallback to string-based detection.
	return needsTTYStringBased(command)
}

// needsTTYFromNodes checks parsed command nodes for interactive commands.
func needsTTYFromNodes(nodes []shellparse.CommandNode, command string) bool {
	for _, node := range nodes {
		if checkTTYCommand(node.Name, node.Args, command) {
			return true
		}
	}
	return false
}

// checkTTYCommand checks if a single command requires TTY.
func checkTTYCommand(base string, args []string, command string) bool {
	switch base {
	case "ssh", "scp", "sftp":
		if strings.Contains(command, "BatchMode=yes") {
			return false
		}
		return true
	case "sudo", "su", "passwd":
		return true
	case "gcloud":
		if len(args) > 0 && (args[0] == "auth" || args[0] == "init") {
			return true
		}
	case "az":
		if len(args) > 0 && args[0] == "login" {
			return true
		}
	case "aws":
		if len(args) > 0 && (args[0] == "sso" || args[0] == "configure") {
			return true
		}
	case "docker":
		if len(args) > 0 && args[0] == "login" {
			return true
		}
	case "gh":
		if len(args) > 0 && args[0] == "auth" {
			return true
		}
	case "npm":
		if len(args) > 0 && args[0] == "login" {
			return true
		}
	case "mysql", "psql", "mongo", "mongosh", "redis-cli":
		return true
	case "ftp", "telnet":
		return true
	case "vi", "vim", "nvim", "nano", "emacs", "pico", "less", "more":
		return true
	}
	return false
}

// needsTTYStringBased is the fallback using simple string parsing.
func needsTTYStringBased(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	base := fields[0]

	switch base {
	case "ssh", "scp", "sftp":
		if strings.Contains(command, "BatchMode=yes") {
			return false
		}
		return true
	case "sudo", "su", "passwd":
		return true
	case "gcloud":
		if len(fields) > 1 && (fields[1] == "auth" || fields[1] == "init") {
			return true
		}
	case "az":
		if len(fields) > 1 && fields[1] == "login" {
			return true
		}
	case "aws":
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
	// Commands that need interactive terminal access cannot run through
	// piped execution. Delegate to the TTY executor if one is available;
	// otherwise reject with a helpful error.
	if NeedsTTY(params.Command) {
		if params.TTYExec != nil {
			return params.TTYExec.ExecuteTTY(ctx, params.Command, params.WorkDir)
		}
		return nil, fmt.Errorf("%w: %q requires user interaction (password, confirmation, etc.). "+
			"The user should run this command directly in their terminal using: !! %s",
			ErrNeedsTTY, params.Command, params.Command)
	}

	// Background execution: start process and return immediately with job ID.
	if params.Background && params.Jobs != nil {
		jobID, err := params.Jobs.Start(ctx, params.Command, params.WorkDir)
		if err != nil {
			return nil, err
		}
		return &ExecResult{
			Stdout: fmt.Sprintf("Background job started: %s\nUse job_id=%q to check output or send signals.", jobID, jobID),
		}, nil
	}

	timeout := DefaultTimeout
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Millisecond
		if timeout > MaxTimeout {
			timeout = MaxTimeout
		}
	} else {
		// Use adaptive timeout based on command type.
		timeout = CommandTimeoutHint(params.Command)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", params.Command)
	if params.WorkDir != "" {
		cmd.Dir = params.WorkDir
	}
	// Create a new process group so signals reach the entire process tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Stdin: use provided content or /dev/null.
	if params.Stdin != "" {
		cmd.Stdin = strings.NewReader(params.Stdin)
	} else {
		cmd.Stdin, _ = os.Open(os.DevNull)
	}

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

// CommandTimeoutHint returns an appropriate timeout duration based on the
// command type. Used when no explicit timeout is provided.
func CommandTimeoutHint(command string) time.Duration {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return DefaultTimeout
	}
	base := fields[0]

	// Quick commands: 30 seconds.
	switch base {
	case "ls", "cat", "head", "tail", "echo", "pwd", "which", "type",
		"date", "whoami", "hostname", "wc", "file", "stat", "true", "false":
		return 30 * time.Second
	}

	// Build/package commands: 5 minutes.
	switch base {
	case "make", "cargo", "go", "npm", "yarn", "pnpm", "mvn", "gradle",
		"pip", "pip3", "apt", "apt-get", "brew", "dnf", "yum", "pacman",
		"gem", "bundle", "rustc", "gcc", "g++", "clang", "javac":
		return 5 * time.Minute
	}

	// Heavy operations: max timeout.
	switch base {
	case "docker", "podman":
		if len(fields) > 1 && (fields[1] == "build" || fields[1] == "pull") {
			return MaxTimeout
		}
	case "wget", "curl":
		return 5 * time.Minute
	}

	return DefaultTimeout
}

// truncateOutput limits output to MaxOutputSize, preserving both head and tail.
func truncateOutput(s string) string {
	return TruncateOutput(s, MaxOutputSize)
}

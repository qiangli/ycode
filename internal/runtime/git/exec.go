package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/toolexec"
)

// GitExec wraps a toolexec.Executor for git operations, providing
// the three-tier fallback (native → host exec → container).
// If executor is nil, falls back to direct os/exec (legacy behavior).
type GitExec struct {
	executor *toolexec.Executor
}

// NewGitExec creates a GitExec. If executor is nil, git commands
// are executed directly via os/exec (no container fallback).
func NewGitExec(executor *toolexec.Executor) *GitExec {
	return &GitExec{executor: executor}
}

// Run executes a git command and returns the combined output.
// This is the single entry point for all git operations in ycode.
func (g *GitExec) Run(ctx context.Context, dir string, args ...string) (string, error) {
	if g.executor == nil {
		return g.directExec(ctx, dir, args...)
	}

	result, err := g.executor.Run(ctx, "git", dir, args...)
	if err != nil {
		return "", err
	}

	// Combine stdout and stderr like CombinedOutput.
	output := result.Stdout
	if result.Stderr != "" && output == "" {
		output = result.Stderr
	}

	return output, nil
}

// RunCheck executes a git command and returns only the error (no output).
func (g *GitExec) RunCheck(ctx context.Context, dir string, args ...string) error {
	_, err := g.Run(ctx, dir, args...)
	return err
}

// RunOutput executes a git command and returns trimmed stdout.
func (g *GitExec) RunOutput(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := g.Run(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// directExec is the legacy fallback when no executor is configured.
func (g *GitExec) directExec(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		if len(args) > 0 {
			return "", fmt.Errorf("git %s: %w", args[0], err)
		}
		return "", fmt.Errorf("git: %w", err)
	}
	return string(out), nil
}

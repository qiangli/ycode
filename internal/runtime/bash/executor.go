package bash

import (
	"context"
)

// Executor defines the interface for executing bash commands.
// Implementations run commands through the host-local execution layer.
type Executor interface {
	Execute(ctx context.Context, params ExecParams) (*ExecResult, error)
}

// TTYExecutor runs commands with full terminal access (PTY).
// Used for interactive commands (ssh, sudo, etc.) that need user input.
// When available, commands that NeedsTTY are delegated here instead of
// being rejected with ErrNeedsTTY.
type TTYExecutor interface {
	ExecuteTTY(ctx context.Context, command string, workDir string) (*ExecResult, error)
}

// HostExecutor runs commands directly on the host OS.
// This is the default executor and provides the existing behavior.
type HostExecutor struct{}

// Execute runs a command on the host.
func (h *HostExecutor) Execute(ctx context.Context, params ExecParams) (*ExecResult, error) {
	return Execute(ctx, params)
}

// ExecuteWith runs a command using the provided executor, falling back to
// direct host execution if executor is nil.
func ExecuteWith(ctx context.Context, executor Executor, params ExecParams) (*ExecResult, error) {
	if executor != nil {
		return executor.Execute(ctx, params)
	}
	return Execute(ctx, params)
}

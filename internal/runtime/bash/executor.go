package bash

import (
	"context"
	"fmt"

	"github.com/qiangli/ycode/internal/container"
)

// Executor defines the interface for executing bash commands.
// Implementations include HostExecutor (direct execution) and
// ContainerExecutor (execution inside a container).
type Executor interface {
	Execute(ctx context.Context, params ExecParams) (*ExecResult, error)
}

// HostExecutor runs commands directly on the host OS.
// This is the default executor and provides the existing behavior.
type HostExecutor struct{}

// Execute runs a command on the host.
func (h *HostExecutor) Execute(ctx context.Context, params ExecParams) (*ExecResult, error) {
	return Execute(ctx, params)
}

// ContainerExecutor runs commands inside a container via podman exec.
type ContainerExecutor struct {
	Container *container.Container
}

// Execute runs a command inside the container.
func (ce *ContainerExecutor) Execute(ctx context.Context, params ExecParams) (*ExecResult, error) {
	if ce.Container == nil {
		return nil, fmt.Errorf("container executor: no container configured")
	}

	result, err := ce.Container.Exec(ctx, params.Command, params.WorkDir)
	if err != nil {
		return nil, err
	}

	return &ExecResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}, nil
}

// ExecuteWith runs a command using the provided executor, falling back to
// direct host execution if executor is nil.
func ExecuteWith(ctx context.Context, executor Executor, params ExecParams) (*ExecResult, error) {
	if executor != nil {
		return executor.Execute(ctx, params)
	}
	return Execute(ctx, params)
}

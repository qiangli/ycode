package agentexec

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// ExecResult holds the outcome of an external agent invocation.
type ExecResult struct {
	// ExitCode is the process exit code (0 = success).
	ExitCode int
	// Stdout is the captured standard output.
	Stdout string
	// Stderr is the captured standard error.
	Stderr string
	// TimedOut indicates the process was killed due to timeout.
	TimedOut bool
	// Duration is how long the process ran.
	Duration time.Duration
	// SessionID is extracted from the output (if available) for session continuity.
	SessionID string
}

// Executor manages the lifecycle of external agent CLI processes.
// Inspired by:
//   - ruflo's headless CLI agent executor (child_process with timeout)
//   - openfang's ProcessManager (ring-buffered output, per-agent limits)
//   - agent-orchestrator's Runtime interface (create/sendMessage/getOutput/isAlive/destroy)
type Executor struct {
	mu      sync.RWMutex
	running map[string]*runningProcess
	logger  *slog.Logger
}

type runningProcess struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	startedAt time.Time
	agentType string
}

// NewExecutor creates an external agent executor.
func NewExecutor(logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		running: make(map[string]*runningProcess),
		logger:  logger,
	}
}

// Run executes an external agent CLI synchronously and returns the result.
// The agent is spawned as a subprocess with captured stdout/stderr.
// If timeout > 0, the process is killed after the timeout expires.
func (e *Executor) Run(ctx context.Context, preset *AgentPreset, opts ExecOptions, timeout time.Duration) (*ExecResult, error) {
	args := preset.BuildCommand(opts)
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command for preset %q", preset.Name)
	}

	// Apply timeout if set.
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	// Set working directory.
	if opts.WorkDir != "" && preset.CwdFlag == "" {
		cmd.Dir = opts.WorkDir
	}

	// Set environment.
	if len(preset.ExtraEnv) > 0 || len(opts.ExtraEnv) > 0 {
		cmd.Env = cmd.Environ()
		for k, v := range preset.ExtraEnv {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		for k, v := range opts.ExtraEnv {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()

	e.logger.Info("agentexec: spawning",
		"agent", preset.Name,
		"command", args[0],
		"args_count", len(args)-1,
	)

	err := cmd.Run()
	duration := time.Since(start)

	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		e.logger.Warn("agentexec: timeout", "agent", preset.Name, "duration", duration)
	} else if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("exec %s: %w", preset.Name, err)
		}
	}

	e.logger.Info("agentexec: completed",
		"agent", preset.Name,
		"exit_code", result.ExitCode,
		"timed_out", result.TimedOut,
		"duration", duration,
		"stdout_len", len(result.Stdout),
	)

	return result, nil
}

// RunningCount returns how many agent processes are currently executing.
func (e *Executor) RunningCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.running)
}

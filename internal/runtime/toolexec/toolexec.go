// Package toolexec provides a two-tier execution framework for external tools.
// When a tool is invoked, the executor tries in order:
//
//  1. Native — in-process Go implementation (e.g., go-git for git operations)
//  2. Host exec — system binary via os/exec (if found on PATH)
//
// Every non-native execution is recorded as a capability gap for the autonomous
// improvement loop to eventually replace with a native implementation.
package toolexec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"

	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// ErrNotImplemented is returned by a NativeFunc to signal that the operation
// should fall through to the next execution tier.
var ErrNotImplemented = errors.New("not implemented natively")

// Tier represents which execution tier handled a command.
type Tier int

const (
	// TierNative means the command was handled in-process (e.g., go-git).
	TierNative Tier = iota
	// TierHostExec means the command was handled by a host binary via exec.
	TierHostExec
	// TierContainer is kept for old telemetry values; container fallback
	// is no longer linked into ycode.
	TierContainer
)

func (t Tier) String() string {
	switch t {
	case TierNative:
		return "native"
	case TierHostExec:
		return "host-exec"
	case TierContainer:
		return "container"
	default:
		return "unknown"
	}
}

// Result holds the output of a tool execution.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Tier     Tier
}

// NativeFunc is an in-process implementation of a subcommand.
// Return ErrNotImplemented to fall through to the next tier.
type NativeFunc func(ctx context.Context, dir string, args []string) (*Result, error)

// ToolDef defines an external tool with its container fallback.
type ToolDef struct {
	// Name is a human-readable identifier (e.g., "git").
	Name string

	// Binary is the host binary name (e.g., "git").
	Binary string

	// Image is the container image tag (e.g., "ycode-git:latest").
	Image string

	// Dockerfile is retained for compatibility with older tool definitions.
	// The lean runtime no longer builds or runs container fallback images.
	Dockerfile string

	// NativeFuncs maps subcommand names to in-process implementations.
	// The first argument of a command is used as the subcommand key.
	NativeFuncs map[string]NativeFunc

	// MountWorkDir mounts the working directory into the container at /workspace.
	MountWorkDir bool
}

// GapRecorder is called when a command executes at a non-native tier.
// The toolexec package does not depend on gaptracker directly — the caller
// wires the recorder at construction time.
type GapRecorder interface {
	RecordGap(ctx context.Context, category, subcommand string, tier Tier)
}

// Executor runs commands through the three-tier fallback chain.
type Executor struct {
	tools    map[string]*ToolDef
	recorder GapRecorder // nil = gap tracking disabled
	mu       sync.RWMutex
}

// New creates an Executor. The first argument is ignored and retained only
// for source compatibility with older callers that passed a container engine.
func New(_ any, recorder GapRecorder) *Executor {
	return &Executor{
		tools:    make(map[string]*ToolDef),
		recorder: recorder,
	}
}

// Register adds a tool definition to the executor.
func (e *Executor) Register(def *ToolDef) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tools[def.Name] = def
}

// Run executes a command through the three-tier fallback chain.
// The toolName must match a registered ToolDef.Name.
func (e *Executor) Run(ctx context.Context, toolName, dir string, args ...string) (*Result, error) {
	e.mu.RLock()
	def, ok := e.tools[toolName]
	e.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("toolexec: unknown tool %q", toolName)
	}

	subcommand := ""
	if len(args) > 0 {
		subcommand = args[0]
	}

	// Tier 1: Native implementation.
	if subcommand != "" && def.NativeFuncs != nil {
		if fn, ok := def.NativeFuncs[subcommand]; ok {
			result, err := fn(ctx, dir, args[1:])
			if err == nil {
				result.Tier = TierNative
				return result, nil
			}
			if !errors.Is(err, ErrNotImplemented) {
				return nil, fmt.Errorf("toolexec: native %s %s: %w", toolName, subcommand, err)
			}
			// Fall through to next tier.
		}
	}

	// Tier 2: Host exec.
	if binaryPath, err := exec.LookPath(def.Binary); err == nil {
		result, err := e.hostExec(ctx, binaryPath, dir, args)
		if err != nil {
			return nil, err
		}
		result.Tier = TierHostExec
		e.recordGap(ctx, def.Name, subcommand, TierHostExec)
		return result, nil
	}

	slog.Info("tool not found on host", "tool", def.Name, "subcommand", subcommand)
	return nil, fmt.Errorf("toolexec: %s not available on host PATH", def.Binary)
}

// hostExec runs the command via os/exec on the host.
func (e *Executor) hostExec(ctx context.Context, binaryPath, dir string, args []string) (*Result, error) {
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = dir

	ctx, finish := telotel.StartExecSpan(ctx, telotel.ExecScopeToolexec, binaryPath, args)
	_ = ctx // span context not propagated further; toolexec calls are single-shot
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			finish(0, err)
			return nil, fmt.Errorf("exec %s: %w", binaryPath, err)
		}
	}
	finish(exitCode, err)

	return &Result{
		Stdout:   string(out),
		ExitCode: exitCode,
	}, nil
}

// recordGap notifies the gap recorder if one is configured.
func (e *Executor) recordGap(ctx context.Context, category, subcommand string, tier Tier) {
	if e.recorder != nil {
		e.recorder.RecordGap(ctx, category, subcommand, tier)
	}
}

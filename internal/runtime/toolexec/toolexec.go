// Package toolexec provides a three-tier execution framework for external tools.
// When a tool is invoked, the executor tries in order:
//
//  1. Native — in-process Go implementation (e.g., go-git for git operations)
//  2. Host exec — system binary via os/exec (if found on PATH)
//  3. Container — auto-build a container image via built-in podman and exec inside it
//
// This allows ycode to run on a barebone host with no external tools installed.
// Every non-native execution is recorded as a capability gap for the autonomous
// improvement loop to eventually replace with a native implementation.
package toolexec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/container"
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
	// TierContainer means the command was handled inside a container.
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

	// Dockerfile is the Dockerfile content for auto-building the container image.
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
	engine   *container.Engine // nil = container tier disabled
	recorder GapRecorder       // nil = gap tracking disabled
	mu       sync.RWMutex

	// containerTools caches containertool-like state per tool (image built once).
	imageBuilt   map[string]bool
	imageBuildMu sync.Mutex
}

// New creates an Executor. Both engine and recorder may be nil.
func New(engine *container.Engine, recorder GapRecorder) *Executor {
	return &Executor{
		tools:      make(map[string]*ToolDef),
		engine:     engine,
		recorder:   recorder,
		imageBuilt: make(map[string]bool),
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

	// Tier 3: Container.
	if e.engine != nil && e.engine.Healthy() {
		slog.Info("tool not found on host, using container fallback",
			"tool", def.Name, "subcommand", subcommand)
		result, err := e.containerExec(ctx, def, dir, args)
		if err != nil {
			return nil, fmt.Errorf("toolexec: container %s: %w", toolName, err)
		}
		result.Tier = TierContainer
		e.recordGap(ctx, def.Name, subcommand, TierContainer)
		return result, nil
	}

	return nil, fmt.Errorf("toolexec: %s not available (no host binary, no container engine)", def.Binary)
}

// hostExec runs the command via os/exec on the host.
func (e *Executor) hostExec(ctx context.Context, binaryPath, dir string, args []string) (*Result, error) {
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("exec %s: %w", binaryPath, err)
		}
	}

	return &Result{
		Stdout:   string(out),
		ExitCode: exitCode,
	}, nil
}

// containerExec runs the command inside a container, building the image if needed.
func (e *Executor) containerExec(ctx context.Context, def *ToolDef, dir string, args []string) (*Result, error) {
	if err := e.ensureImage(ctx, def); err != nil {
		return nil, fmt.Errorf("ensure image %s: %w", def.Image, err)
	}

	// Build the command string for sh -c inside the container.
	cmdParts := append([]string{def.Binary}, args...)
	cmdStr := strings.Join(cmdParts, " ")

	// Create container with workspace mounted.
	var mounts []container.Mount
	if def.MountWorkDir && dir != "" {
		mounts = append(mounts, container.Mount{
			Source:   dir,
			Target:   "/workspace",
			ReadOnly: false,
		})
	}

	cfg := &container.ContainerConfig{
		Image:   def.Image,
		Command: []string{"cat"}, // keep alive for exec
		Mounts:  mounts,
		WorkDir: "/workspace",
	}

	ctr, err := e.engine.CreateContainer(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}
	defer ctr.Remove(ctx, true)

	if err := ctr.Start(ctx); err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	execResult, err := ctr.Exec(ctx, cmdStr, "/workspace")
	if err != nil {
		return nil, fmt.Errorf("exec in container: %w", err)
	}

	return &Result{
		Stdout:   execResult.Stdout,
		Stderr:   execResult.Stderr,
		ExitCode: execResult.ExitCode,
	}, nil
}

// ensureImage builds the container image if it hasn't been built yet.
func (e *Executor) ensureImage(ctx context.Context, def *ToolDef) error {
	e.imageBuildMu.Lock()
	defer e.imageBuildMu.Unlock()

	if e.imageBuilt[def.Image] {
		return nil
	}

	if e.engine.ImageExists(ctx, def.Image) {
		e.imageBuilt[def.Image] = true
		return nil
	}

	slog.Info("building container tool image (one-time)",
		"tool", def.Name, "image", def.Image)

	buildCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if err := e.engine.BuildImage(buildCtx, def.Image, []byte(def.Dockerfile)); err != nil {
		return err
	}

	e.imageBuilt[def.Image] = true
	slog.Info("container tool image built", "tool", def.Name, "image", def.Image)
	return nil
}

// recordGap notifies the gap recorder if one is configured.
func (e *Executor) recordGap(ctx context.Context, category, subcommand string, tier Tier) {
	if e.recorder != nil {
		e.recorder.RecordGap(ctx, category, subcommand, tier)
	}
}

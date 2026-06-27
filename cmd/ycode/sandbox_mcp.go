package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	container "github.com/qiangli/coreutils/external/podman/engine"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// MCPHandler exposes podman-isolated execution to external coding agents over
// MCP (the `sandbox_exec` tool). The engine itself now lives in coreutils
// (external/podman/engine); this is the ycode-specific MCP glue that stays here.
type MCPHandler struct{}

func NewMCPHandler() *MCPHandler { return &MCPHandler{} }

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{{
		Name: "sandbox_exec",
		Description: "Run a command in a podman-isolated sandbox. By default: " +
			"image=alpine:3.21, network=none, cwd mounted read-write at /workspace, " +
			"container removed on exit. Returns a JSON envelope with stdout, stderr, " +
			"exit_code, and duration_ms. Use for executing untrusted or AI-generated " +
			"code without exposing the host filesystem or network.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command":    {"type": "array", "items": {"type": "string"}, "description": "argv to exec inside the container, e.g. [\"sh\", \"-c\", \"echo hi\"]."},
				"image":      {"type": "string", "description": "Container image. Default alpine:3.21."},
				"workdir":    {"type": "string", "description": "Host directory to mount at /workspace. Default: the MCP server's cwd."},
				"network":    {"type": "string", "description": "Podman network mode. Default \"none\" (no network)."},
				"timeout_ms": {"type": "integer", "description": "Per-call timeout in milliseconds. 0 = no limit."}
			},
			"required": ["command"]
		}`),
	}}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

func (h *MCPHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeDangerFullAccess
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	if name != "sandbox_exec" {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	var args struct {
		Command   []string `json:"command"`
		Image     string   `json:"image"`
		Workdir   string   `json:"workdir"`
		Network   string   `json:"network"`
		TimeoutMS int      `json:"timeout_ms"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if len(args.Command) == 0 {
		return "", fmt.Errorf("command is required (and must be a non-empty array)")
	}

	image := args.Image
	if image == "" {
		image = "alpine:3.21"
	}
	workdir := args.Workdir
	if workdir == "" {
		var err error
		workdir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
	}
	network := args.Network
	if network == "" {
		network = "none"
	}

	if args.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(args.TimeoutMS)*time.Millisecond)
		defer cancel()
	}

	eng, err := container.SharedEngine(ctx)
	if err != nil {
		return "", fmt.Errorf("container engine unavailable: %w", err)
	}

	cfg := &container.ContainerConfig{
		Image:   image,
		Network: network,
		WorkDir: "/workspace",
		Mounts: []container.Mount{{
			Source: workdir,
			Target: "/workspace",
		}},
		Command: args.Command,
	}

	ctx, finish := telotel.StartExecSpan(ctx, telotel.ExecScopeSandbox, "embedded-podman", append([]string{image}, args.Command...))
	start := time.Now()
	result, runErr := eng.RunOneShot(ctx, cfg)
	duration := time.Since(start)

	exitCode := -1
	stdoutStr := ""
	stderrStr := ""
	if result != nil {
		exitCode = result.ExitCode
		stdoutStr = result.Stdout
		stderrStr = result.Stderr
	}
	finish(exitCode, runErr)

	envelope := struct {
		Stdout     string `json:"stdout"`
		Stderr     string `json:"stderr"`
		ExitCode   int    `json:"exit_code"`
		DurationMS int64  `json:"duration_ms"`
		Error      string `json:"error,omitempty"`
	}{
		Stdout:     stdoutStr,
		Stderr:     stderrStr,
		ExitCode:   exitCode,
		DurationMS: duration.Milliseconds(),
	}
	if runErr != nil {
		envelope.Error = runErr.Error()
	}

	out, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal envelope: %w", err)
	}
	return string(out), nil
}

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("no resources: %s", uri)
}

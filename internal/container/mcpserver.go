package container

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// MCPHandler exposes ycode's podman-isolated execution to external
// coding agents over MCP. Mirrors the `yc sandbox -- <cmd>` shell
// built-in: alpine by default, cwd mounted at /workspace, network
// disabled. One-shot — the container is removed on exit.
//
// Foreign agents typically call this when they need to execute
// untrusted code (e.g. AI-generated scripts) without giving the model
// access to the host filesystem or network. Requires `podman` on the
// host PATH (or running inside `ycode serve`, which embeds it).
type MCPHandler struct{}

// NewMCPHandler builds the handler. Stateless — each call spawns a
// fresh `podman run --rm` invocation.
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

// RequiredMode: sandbox_exec can run arbitrary commands inside the
// container. The host is isolated by podman, but from the agent's
// permission perspective this is still arbitrary code execution —
// classify as DangerFullAccess so the gate stays explicit.
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

	if _, err := exec.LookPath("podman"); err != nil {
		return "", fmt.Errorf("podman not found in PATH (install podman or run inside `ycode serve`)")
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

	podmanArgs := []string{
		"run", "--rm",
		"--network=" + network,
		"--volume", fmt.Sprintf("%s:/workspace:rw", workdir),
		"--workdir", "/workspace",
		image,
	}
	podmanArgs = append(podmanArgs, args.Command...)

	cmd := exec.CommandContext(ctx, "podman", podmanArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	ctx, finish := telotel.StartExecSpan(ctx, telotel.ExecScopeSandbox, "podman", podmanArgs)
	_ = ctx
	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	finish(exitCode, runErr)

	envelope := struct {
		Stdout     string `json:"stdout"`
		Stderr     string `json:"stderr"`
		ExitCode   int    `json:"exit_code"`
		DurationMS int64  `json:"duration_ms"`
		Error      string `json:"error,omitempty"`
	}{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ExitCode:   exitCode,
		DurationMS: duration.Milliseconds(),
	}
	if runErr != nil && exitCode < 0 {
		// Non-exit errors (timeout, podman binary missing mid-run, etc.)
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

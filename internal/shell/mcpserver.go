package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// MCPHandler exposes ycode shell's agent posture as an MCP tool. Foreign
// coding agents that already speak MCP get the same `--agent --json -c`
// surface as a single tool call — no need to spawn ycode shell as a
// subprocess and parse stdout.
//
// The handler runs each request through DispatchEnvelope, which produces
// the JSON envelope (stdout, stderr, exit_code, duration_ms, intent,
// hints[]). Result text is the marshaled envelope.
type MCPHandler struct {
	rt *ShellRuntime
	// suggest is the pre-exec hint engine, populated via SetSuggestFunc.
	// We re-use the package-level suggestFn rather than depending on
	// internal/shell/agentmode (avoids the cycle).
}

// NewMCPHandler builds an MCP handler bound to a shared ShellRuntime.
// Caller is responsible for installing skill resolver, registry,
// provider, and the yc-builtins / agentmode init() side effects.
func NewMCPHandler(rt *ShellRuntime) *MCPHandler { return &MCPHandler{rt: rt} }

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{{
		Name: "agent_shell",
		Description: "Run a shell command via ycode shell with --agent --json semantics. " +
			"Returns a JSON envelope with stdout, stderr, exit_code, duration_ms, " +
			"intent metadata, and any hints emitted by the agent-mode catalog (e.g. " +
			"suggesting `yc search-symbols` when the agent ran `grep -r`). " +
			"Sentinels (/, @, !, ?) work in `command` exactly like the interactive shell. " +
			"yc <verb> built-ins (symbols, search-symbols, refs, repomap, graph, git, " +
			"remember, recall, browser, sandbox) run in-process — no PATH lookup needed.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command":  {"type": "string", "description": "The bash command (or sentinel form) to dispatch."},
				"cwd":      {"type": "string", "description": "Absolute working directory for this call. When omitted, the call runs in the ycode shell server's cwd; HTTP MCP callers should always pass this so commands run in their project root, not in ycode serve's launch directory."},
				"hints":    {"type": "boolean", "description": "Emit agent-mode hints. Default true."},
				"timeout_ms": {"type": "integer", "description": "Optional per-call timeout in milliseconds."}
			},
			"required": ["command"]
		}`),
	}}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

// RequiredMode reports the permission tier needed to run a tool. The
// shell tool can do anything bash can — DangerFullAccess is the right
// posture, same as `ycode shell -c`.
func (h *MCPHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeDangerFullAccess
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	if name != "agent_shell" {
		return "", fmt.Errorf("mcp shell: unknown tool %q", name)
	}
	var args struct {
		Command   string `json:"command"`
		Cwd       string `json:"cwd,omitempty"`
		Hints     *bool  `json:"hints,omitempty"`
		TimeoutMS int    `json:"timeout_ms,omitempty"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if args.Command == "" {
		return "", fmt.Errorf("mcp agent_shell: command is required")
	}

	if args.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, durationFromMS(args.TimeoutMS))
		defer cancel()
	}

	wantHints := true
	if args.Hints != nil {
		wantHints = *args.Hints
	}
	var hints []Hint
	if wantHints {
		hints = Suggestions(h.rt, args.Command)
	}

	env := DispatchEnvelopeAt(ctx, h.rt, args.Command, hints, args.Cwd)
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal envelope: %w", err)
	}
	return string(out), nil
}

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("mcp shell: no resources (asked for %q)", uri)
}

func durationFromMS(ms int) time.Duration {
	return time.Duration(ms) * time.Millisecond
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// cmdmcp.go — the cobra→MCP runner. Exposes every safe `ycode <verb>`
// as MCP-callable so foreign agents can invoke ycode CLI capabilities
// without shelling out manually.
//
// ============================================================================
// SAFEGUARDS — read before changing the allowlist
// ============================================================================
//
//  1. DENY BY DEFAULT. A verb is callable via MCP ONLY if it appears in
//     cobraAllowlist below with an explicit permission tier. Adding an
//     entry is a deliberate act: think about side effects on disk,
//     network, running processes, and other agents. When in doubt, do
//     not add it — ask the operator to shell out via agent_shell with
//     the appropriate permission gate instead.
//
//  2. CHILD-PROCESS ISOLATION. Every call exec's a fresh `os.Executable()`
//     subprocess. We do NOT re-enter rootCmd in-process because cobra's
//     SetOut/SetArgs are package-globals and concurrent MCP calls would
//     race. Subprocess also gives natural exit codes and PID-scoped
//     cleanup on timeout.
//
//  3. HARD TIMEOUT. Default 30s, max 300s (5 min). Verbs that legitimately
//     stream forever (serve, ralph, loop, train, eval, autopilot) MUST NOT
//     appear in the allowlist — the runner is for transactional commands.
//
//  4. PERMISSION TIER PER VERB. RequiredMode() reports the *handler's*
//     ceiling (the higher of the two run-tool tiers), but inside the
//     handler we re-check the verb's declared tier and reject calls that
//     exceed the tool the caller selected. So a stdio client at the
//     default ReadOnly ceiling can only ever invoke ReadOnly verbs even
//     though run_ycode_command itself is callable.
//
//  5. NEVER ADD interactive verbs (login, prompt), self-recursive verbs
//     (prompt, ralph, loop, auto, autopilot, collab, mesh start),
//     internal verbs (runner, shell-trace, internal-*), or PATH-mutating
//     verbs (wrap install) to the allowlist. These break the runner's
//     transactional contract or create cycles.
//
// ============================================================================

// cmdAllowEntry is one verb the runner is allowed to invoke. The Verb
// is the first positional arg as it appears in cobra (e.g. "doctor",
// "model" — sub-subcommands like "model list" come in via the args
// slice). The Mode is the lowest permission tier a caller must hold to
// run this verb. Help is a short, agent-facing description that surfaces
// in list_ycode_commands so foreign agents pick the right verb.
type cmdAllowEntry struct {
	Verb string
	Mode mcp.PermissionMode
	Help string
}

// cobraAllowlist is the explicit set of verbs reachable via MCP. ORDER
// DOES NOT MATTER for execution, but keep it grouped by intent so it's
// reviewable. Default principle: read-only verbs get ReadOnly; verbs
// that mutate workspace or local state get WorkspaceWrite; nothing gets
// DangerFullAccess via this surface (use agent_shell for that).
//
// Sub-subcommands are filtered separately by subAllowlist below — e.g.
// "model" is allowed but inside we restrict to "model list" / "model
// available", not "model pull" (which downloads gigabytes).
var cobraAllowlist = []cmdAllowEntry{
	// --- ReadOnly: pure introspection, no side effects ---
	{"version", mcp.ModeReadOnly, "Print the ycode binary version."},
	{"doctor", mcp.ModeReadOnly, "Run health checks (API keys, storage backends, dependencies)."},
	{"docs", mcp.ModeReadOnly, "Print agent-facing capability prompts. With no arg, prints the topic index."},
	{"features", mcp.ModeReadOnly, "List ycode's feature registry (build tiers, stability)."},
	{"model", mcp.ModeReadOnly, "Inspect locally-available LLM models. Use args=[\"list\"] or [\"available\"]."},
	{"config", mcp.ModeReadOnly, "Read settings.json. Use args=[\"get\", \"<field>\"] or [\"show\"]."},
	{"tasks", mcp.ModeReadOnly, "Inspect the multi-agent task queue. Use args=[\"list\"]."},
	{"backlog", mcp.ModeReadOnly, "Inspect docs/backlog/. Use args=[\"list\"] or [\"status\"]."},
	{"foreman", mcp.ModeReadOnly, "Inspect Foreman state. Use args=[\"status\"] only — start/pause require WorkspaceWrite."},
	{"skill", mcp.ModeReadOnly, "Inspect installed skills. Use args=[\"list\"]."},
	{"pair", mcp.ModeReadOnly, "Print the bearer-token + config snippet for a foreign tool. Use args=[\"--tool\", \"<name>\"]."},
	{"netscan", mcp.ModeReadOnly, "Discover ycode servers on the local network."},

	// --- WorkspaceWrite: mutates project / on-disk state ---
	{"init", mcp.ModeWorkspaceWrite, "Establish ycode in the current git repo (writes .agents/ycode/AGENTS.md)."},
	{"heal", mcp.ModeWorkspaceWrite, "Self-healing subcommands. Use args=[\"status\"] for read-only inspection."},
}

// subAllowlist constrains sub-subcommands for verbs whose top-level is
// allowlisted but where individual subcommands have very different
// permission profiles. The map key is the top-level verb; the value is
// the set of first-arg tokens allowed below it. An empty (nil) set means
// "all sub-subcommands allowed at the verb's declared tier". A verb
// listed in cobraAllowlist but absent from this map is treated as nil
// (no restriction beyond the verb's tier).
//
// IMPORTANT: when a sub-subcommand fundamentally changes the verb's
// risk profile (e.g. `model pull` vs `model list`), restrict here
// rather than promoting the whole verb to a higher tier — that way the
// read-side stays cheap for foreign agents.
var subAllowlist = map[string]map[string]bool{
	"model":   {"list": true, "available": true, "info": true},
	"config":  {"get": true, "show": true, "list": true},
	"tasks":   {"list": true, "status": true},
	"backlog": {"list": true, "status": true, "show": true},
	"foreman": {"status": true},
	"skill":   {"list": true},
	"heal":    {"status": true},
}

// runnerMaxTimeoutSeconds caps the per-call timeout. A verb that
// legitimately runs longer than five minutes belongs in a `ycode serve`
// background process or a `ycode loop`, not in a transactional MCP call.
const runnerMaxTimeoutSeconds = 300

// runnerDefaultTimeoutSeconds is the timeout applied when the caller
// omits timeout_seconds. Most allowlisted verbs complete in well under
// 5s; 30s gives generous headroom for cold-start cases.
const runnerDefaultTimeoutSeconds = 30

// cobraMCPHandler implements mcp.ServerHandler and mcp.PermissionAware.
type cobraMCPHandler struct{}

func newCobraMCPHandler() *cobraMCPHandler { return &cobraMCPHandler{} }

func (h *cobraMCPHandler) ListTools() []mcp.Tool {
	commonInputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"verb": {"type": "string", "description": "Cobra verb (first positional arg). See list_ycode_commands for the allowlist."},
			"args": {"type": "array", "items": {"type": "string"}, "description": "Additional args including flags. Subcommands and flags pass through to cobra."},
			"timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 300, "description": "Hard timeout. Default 30, max 300."}
		},
		"required": ["verb"]
	}`)
	return []mcp.Tool{
		{
			Name: "list_ycode_commands",
			Description: "List the ycode CLI verbs callable via MCP. Returns an array of " +
				"{verb, mode, help, sub_allowlist} entries. Call this first to discover " +
				"what `run_ycode_command` and `run_ycode_command_workspace` accept.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
		{
			Name: "run_ycode_command",
			Description: "Invoke a ReadOnly ycode CLI verb (e.g. doctor, version, model list, " +
				"docs, features). Returns {stdout, stderr, exit_code, duration_ms} JSON. " +
				"Verbs with WorkspaceWrite tier must use run_ycode_command_workspace.",
			InputSchema: commonInputSchema,
		},
		{
			Name: "run_ycode_command_workspace",
			Description: "Invoke a WorkspaceWrite ycode CLI verb (e.g. init, heal status). " +
				"Same envelope as run_ycode_command. Requires WorkspaceWrite gate.",
			InputSchema: commonInputSchema,
		},
	}
}

func (h *cobraMCPHandler) ListResources() []mcp.Resource { return nil }

func (h *cobraMCPHandler) ReadResource(ctx context.Context, uri string) (string, error) {
	return "", fmt.Errorf("cobraMCPHandler: no resources exposed")
}

func (h *cobraMCPHandler) RequiredMode(toolName string) mcp.PermissionMode {
	switch toolName {
	case "list_ycode_commands":
		return mcp.ModeReadOnly
	case "run_ycode_command":
		return mcp.ModeReadOnly
	case "run_ycode_command_workspace":
		return mcp.ModeWorkspaceWrite
	default:
		// Unknown tool — return the highest tier so the gate denies it
		// safely. The handler dispatch will return "unknown tool".
		return mcp.ModeDangerFullAccess
	}
}

type runArgs struct {
	Verb           string   `json:"verb"`
	Args           []string `json:"args"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type runResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out,omitempty"`
}

func (h *cobraMCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "list_ycode_commands":
		return formatCmdAllowlist()
	case "run_ycode_command":
		return h.runVerb(ctx, input, mcp.ModeReadOnly)
	case "run_ycode_command_workspace":
		return h.runVerb(ctx, input, mcp.ModeWorkspaceWrite)
	default:
		return "", fmt.Errorf("unknown tool: %q", name)
	}
}

// runVerb is the shared dispatcher behind both run_ycode_command and
// run_ycode_command_workspace. callerTier is the tier the calling tool
// is gated at; we reject calls to verbs whose declared tier exceeds it.
func (h *cobraMCPHandler) runVerb(ctx context.Context, input json.RawMessage, callerTier mcp.PermissionMode) (string, error) {
	var args runArgs
	if len(input) > 0 && !isEmptyRawJSON(input) {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}
	args.Verb = strings.TrimSpace(args.Verb)
	if args.Verb == "" {
		return "", errors.New("verb is required (see list_ycode_commands)")
	}

	entry, ok := lookupCobraEntry(args.Verb)
	if !ok {
		return "", fmt.Errorf("verb %q is not allowlisted (call list_ycode_commands to see allowed verbs)", args.Verb)
	}

	// Per-verb tier check: a ReadOnly tool must not be used to run a
	// WorkspaceWrite verb, even though the gate already accepted the
	// call. Hand the caller a precise error so they know which tool to
	// switch to.
	if rank(entry.Mode) > rank(callerTier) {
		needed := "run_ycode_command_workspace"
		return "", fmt.Errorf("verb %q requires %s; call %s instead", args.Verb, entry.Mode, needed)
	}

	// Sub-subcommand allowlist (per safeguard #5). When defined, the
	// first non-flag token in args must appear in the allowed set. We
	// only inspect the FIRST positional arg — deeper restrictions belong
	// in the cobra command itself, not here.
	if allowedSubs, hasRestriction := subAllowlist[args.Verb]; hasRestriction && len(allowedSubs) > 0 {
		firstPositional := firstNonFlagArg(args.Args)
		if firstPositional == "" || !allowedSubs[firstPositional] {
			allowed := sortedKeys(allowedSubs)
			return "", fmt.Errorf("verb %q restricts subcommands to %v; got %q",
				args.Verb, allowed, firstPositional)
		}
	}

	timeout := time.Duration(args.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = runnerDefaultTimeoutSeconds * time.Second
	}
	if timeout > runnerMaxTimeoutSeconds*time.Second {
		timeout = runnerMaxTimeoutSeconds * time.Second
	}

	result, err := execCobraVerb(ctx, args.Verb, args.Args, timeout)
	if err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// execCobraVerb spawns `ycode <verb> <args...>` as a subprocess with a
// hard timeout. Returns the captured envelope. A non-zero exit from the
// child is NOT a Go error — it's reported via runResult.ExitCode so
// callers can interpret it. The only Go errors returned are launch
// failures (binary not found, permission denied).
func execCobraVerb(ctx context.Context, verb string, extra []string, timeout time.Duration) (*runResult, error) {
	binary, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve ycode binary: %w", err)
	}

	cmdArgs := append([]string{verb}, extra...)
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, binary, cmdArgs...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	// Inherit env so the subprocess sees ANTHROPIC_API_KEY etc. PATH is
	// preserved via the inherited environment.
	cmd.Env = os.Environ()

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	result := &runResult{
		Stdout:     outBuf.String(),
		Stderr:     errBuf.String(),
		DurationMs: elapsed.Milliseconds(),
	}

	if execCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		return result, nil
	}

	if runErr == nil {
		result.ExitCode = 0
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	// Anything else is a launch failure (binary missing, permission denied).
	return nil, fmt.Errorf("launch %s: %w", binary, runErr)
}

func formatCmdAllowlist() (string, error) {
	type row struct {
		Verb         string   `json:"verb"`
		Mode         string   `json:"mode"`
		Help         string   `json:"help"`
		SubAllowlist []string `json:"sub_allowlist,omitempty"`
	}
	out := make([]row, 0, len(cobraAllowlist))
	for _, e := range cobraAllowlist {
		r := row{Verb: e.Verb, Mode: string(e.Mode), Help: e.Help}
		if subs, ok := subAllowlist[e.Verb]; ok {
			r.SubAllowlist = sortedKeys(subs)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Verb < out[j].Verb })
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func lookupCobraEntry(verb string) (cmdAllowEntry, bool) {
	for _, e := range cobraAllowlist {
		if e.Verb == verb {
			return e, true
		}
	}
	return cmdAllowEntry{}, false
}

func firstNonFlagArg(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isEmptyRawJSON(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "{}"
}

// rank mirrors the unexported PermissionMode.rank() so we can compare
// tiers locally without exporting the method on the upstream type.
func rank(m mcp.PermissionMode) int {
	switch m {
	case mcp.ModeReadOnly:
		return 1
	case mcp.ModeWorkspaceWrite:
		return 2
	case mcp.ModeDangerFullAccess:
		return 3
	}
	return 0
}

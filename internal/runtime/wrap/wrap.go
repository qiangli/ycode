// Package wrap implements `ycode wrap` — a PATH-shim that launches a
// third-party agentic tool (Claude Code, Codex, Aider, Gemini CLI,
// opencode, ...) with its shell-out commands routed through ycode for
// OTel observability and best-effort policy.
//
// Two roles in a single package:
//
//  1. Run(ctx, Options) is the parent side: materialize a shim
//     directory under $XDG_RUNTIME_DIR/ycode-wrap/<pid>/bin/,
//     prepend it to PATH, set SHELL to the bash shim, launch the
//     foreign agent process, and tear the shim down on exit.
//
//  2. ShimMain() is the child side: invoked when ycode is exec'd via
//     a shim symlink (argv[0] basename != "ycode" AND YCODE_WRAP_SHIM
//     is set). Increments the YCODE_WRAP_DEPTH recursion guard,
//     strips the shim directory from PATH, resolves the real binary,
//     wraps the call in an ExecScopeWrappedAgent OTel span, and
//     exec's the real command with stdin/out/err inherited.
//
// Documented limit: foreign agents that build exec.Command with an
// absolute path (e.g. "/bin/bash") bypass the shim entirely. Ring 1
// is observability + best-effort policy, not a security boundary —
// Ring 2 (Landlock + seccomp_unotify on Linux 5.10+) owns that.
package wrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	hookruntime "github.com/qiangli/ycode/internal/runtime/wrap/runtime"
	telotel "github.com/qiangli/ycode/internal/telemetry/otel"

	"github.com/qiangli/ycode/internal/runtime/spawncore"
)

// envShim signals to ycode's main() that the process was invoked via a
// shim symlink and should dispatch to ShimMain rather than the normal
// cobra root. Without this gate, renaming the ycode binary to "bash"
// would silently engage shim behavior — the env var makes shim mode
// an opt-in contract between Run() and ShimMain().
const envShim = spawncore.EnvShim

// envDepth is a recursion guard: every shim invocation increments the
// counter; ShimMain refuses to re-enter past maxShimDepth. Without
// this, a bash shim that spawns a sub-bash through PATH would loop
// forever.
const envDepth = spawncore.EnvDepth

// envShimDir is the absolute path of the shim directory the parent
// materialized. The child needs this to strip the shim from PATH
// before resolving the real binary (otherwise the resolve would
// re-hit the shim).
const envShimDir = spawncore.EnvShimDir

// envWrappedAgent records the foreign-agent program name (claude,
// codex, aider, gemini, opencode, ...) so OTel spans can carry it as
// an attribute. Set by Run; read by ShimMain.
const envWrappedAgent = "YCODE_WRAP_AGENT"

const maxShimDepth = spawncore.MaxDepth

// defaultShims is the list of command names a fresh `ycode wrap`
// session materializes symlinks for. Each entry must be a basename
// only — the shim directory becomes the parent agent's $PATH prefix
// so the shim names must match what the agent expects to find on
// $PATH. New entries are cheap: each is one extra symlink.
var defaultShims = []string{
	"bash", "sh", "dash", "zsh",
	"rg", "find", "grep", "sed", "awk",
	"cat", "head", "tail", "wc", "tree",
	"git",
	"curl", "wget",
	"npm", "pip", "pip3", "python", "python3", "node",
	"gh",
	"jq",
}

// Options configures a wrap invocation.
type Options struct {
	// AgentArgs is the foreign-agent command plus its args, e.g.
	// ["claude", "-p", "explain this repo"]. Required.
	AgentArgs []string

	// WorkDir is the cwd the foreign agent runs in. Defaults to the
	// caller's cwd. The Loom auto-lease flow (Phase 2) lands a fresh
	// workspace path here; today the caller passes its own cwd.
	WorkDir string

	// Permission is the foreign agent's permission ceiling. One of
	// "read-only", "workspace-write", "danger-full-access" (default).
	// Reserved for future plumbing into the shim dispatcher's VFS
	// boundary check.
	Permission string

	// Loom selects the Loom workspace mode. "off" (default) runs in
	// WorkDir as-is; "auto"/"on" delegates workspace allocation to
	// LoomLeaser (when configured), which is the v2 auto-attach path.
	// When LoomLeaser is nil, a one-line warn is logged and the wrap
	// proceeds without leasing.
	Loom string

	// LoomLeaser, when non-nil and Loom == "auto"/"on", allocates a
	// fresh Loom workspace before launching the foreign agent. The
	// returned env vars (YCODE_LOOM_*) are merged into the agent's
	// environment, and WorkDir is overridden to the sandbox path; the
	// agent therefore wakes up inside its isolated workspace without
	// knowing Loom exists. The Release callback is invoked when the
	// agent exits (cleanly or not).
	//
	// The interface is pluggable so the wrap parent can be embedded in
	// `ycode serve` (calls pkg/loom.Service directly), the standalone
	// `ycode wrap` CLI (calls Service via HTTP-MCP against a running
	// serve), and tests (fake leaser asserting the env shape).
	LoomLeaser LoomLeaser

	// AllowedDirs is reserved for the Phase 2 VFS boundary that the
	// shim dispatcher will consult before exec'ing. Today it is
	// stored in env for the child but not enforced.
	AllowedDirs []string

	// ExtraShims appends to the default shim catalog. Use for agents
	// that shell out to project-specific tooling.
	ExtraShims []string

	// Profile is the per-agent profile key to resolve in AgentProfiles.
	// When empty, ResolveProfile auto-detects from AgentArgs[0] basename
	// (so `ycode wrap claude-code` matches the "claude" profile).
	// An explicit value that does not match a known profile is an error
	// — the caller must surface it before invoking Run.
	Profile string

	// RuntimeHooks lists language patchers to install for the wrapped
	// agent process ("python", "node"). Default off unless populated
	// either by a matched profile or by an explicit CLI flag. Honored
	// by Piece D; today the field is stored but no hook materialization
	// happens.
	RuntimeHooks []string

	// OTelExport selects the wrap-parent's OTel local sink:
	// "file" (default), "console", or "off". Empty resolves to file.
	// The YCODE_WRAP_OTEL_EXPORT env always wins when set —
	// ParseExportMode handles both the flag and the env.
	OTelExport string

	// PTY selects how stdio is plumbed:
	//   "auto"   — PTY when both stdin and stdout are terminals.
	//   "always" — PTY regardless.
	//   "never"  — inherit-FD always.
	// Empty resolves to auto. See ParsePTYMode.
	PTY string

	// Env is the base environment passed to the foreign agent.
	// Defaults to os.Environ() when nil. PATH, SHELL, and the
	// YCODE_WRAP_* coordination env are overwritten.
	Env []string

	Stdin          io.Reader
	Stdout, Stderr io.Writer

	// LogPath, when non-empty, is the directory the shim writes its
	// own session log to (one line per shim invocation). Defaults to
	// the shim directory itself.
	LogPath string
}

// Run materializes the shim directory, launches the foreign agent
// with PATH/SHELL/env pointed at it, blocks until the agent exits,
// then tears the shim down. Returns the agent's exit code.
//
// The Loom auto-lease integration is a Phase 2 follow-up — the
// scaffold is in place (Loom field) but today the foreign agent
// runs in WorkDir as-is.
func Run(ctx context.Context, opts Options) (int, error) {
	defer initLoggerFromEnv()()
	if len(opts.AgentArgs) == 0 {
		return 1, errors.New("wrap.Run: AgentArgs is required")
	}
	if opts.WorkDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return 1, fmt.Errorf("wrap.Run: getwd: %w", err)
		}
		opts.WorkDir = wd
	}
	if opts.Loom != "" && opts.Loom != "off" {
		if opts.LoomLeaser == nil {
			slog.Warn("wrap: --loom=auto/on requested but no LoomLeaser configured; running without Loom workspace",
				"loom", opts.Loom)
		} else {
			label := inferLoomLabel(opts.AgentArgs)
			loomID, sandbox, branch, leaseEnv, release, err := opts.LoomLeaser.LeaseForWrap(ctx, opts.WorkDir, label)
			if err != nil {
				return 1, fmt.Errorf("wrap: loom lease: %w", err)
			}
			defer release()
			opts.WorkDir = sandbox
			env := opts.Env
			if env == nil {
				env = os.Environ()
			}
			env = append(env,
				"YCODE_LOOM_ID="+loomID,
				"YCODE_LOOM_BRANCH="+branch,
				"YCODE_LOOM_LABEL="+label,
			)
			for k, v := range leaseEnv {
				env = append(env, k+"="+v)
			}
			opts.Env = env
			slog.Info("wrap: loom workspace attached",
				"loom_id", loomID, "sandbox", sandbox, "branch", branch, "label", label)
		}
	}

	// Resolve a per-agent profile and merge its defaults into opts.
	// Explicit --profile that does not match a registered agent is an
	// error so the user sees the typo immediately; auto-detect failure
	// is silent (the wrap still runs, just without profile defaults).
	if opts.Profile != "" {
		profile, ok := ResolveProfile(opts.Profile, opts.AgentArgs)
		if !ok {
			return 1, fmt.Errorf("wrap.Run: unknown --profile %q (known: %v)", opts.Profile, ProfileNames())
		}
		profile.Apply(&opts)
	} else if profile, ok := ResolveProfile("", opts.AgentArgs); ok {
		profile.Apply(&opts)
	}

	// Per-profile limitation warnings. Route through opts.Stderr (falling
	// back to os.Stderr when the caller didn't override it) so tests and
	// embedded uses can capture the notice in their own buffer.
	noticeOut := opts.Stderr
	if noticeOut == nil {
		noticeOut = os.Stderr
	}

	// Codex limitation warning — the Rust runtime has no language-level
	// hook, so absolute-path shell-outs from the Codex binary itself
	// bypass tracing. The PATH shim still catches what it can.
	if isResolvedProfile(&opts, "codex") {
		_, _ = fmt.Fprintln(noticeOut,
			"[ycode wrap] codex: Rust runtime — no language-level hook; "+
				"shell-outs via absolute paths bypass tracing. PATH-shim coverage only.")
	}

	// Claude Code limitation warning — Bun-compiled binary doesn't
	// honor NODE_OPTIONS=--require, so the Node runtime hook would be
	// a no-op. The PATH shim still catches bash/git/rg/etc., and the
	// supported integration path remains MCP via ~/.claude.json (see
	// internal/selfinit/claude.go and the repo-root .mcp.json).
	if isResolvedProfile(&opts, "claude") {
		_, _ = fmt.Fprintln(noticeOut,
			"[ycode wrap] claude: Bun runtime — NODE_OPTIONS not honored; "+
				"PATH-shim coverage only. MCP integration via .mcp.json / ~/.claude.json is the supported path.")
	}

	// Install the wrap-parent's OTel exporter (file / console / off)
	// before opening any span so the first StartExecSpan call lands
	// in the configured sink. The shutdown closure is deferred so
	// exporters flush on normal Run exit; SIGKILL'd processes lose
	// in-flight spans, same trade-off the main app accepts.
	otelShutdown := setupOTel(ctx, ParseExportMode(opts.OTelExport),
		exportEnv("YCODE_WRAP_AGENT", filepath.Base(opts.AgentArgs[0])),
		exportEnv("YCODE_WRAP_PROFILE", resolvedProfileName(&opts)),
	)
	defer otelShutdown()

	self, err := os.Executable()
	if err != nil {
		return 1, fmt.Errorf("wrap.Run: locate ycode binary: %w", err)
	}

	// Reap any per-PID shim dirs left over from earlier sessions that
	// crashed without cleanup. Best-effort — failure here doesn't block
	// the new session.
	reapStaleShimDirs(chooseShimRoot())

	shimDir, sessionDir, err := materializeShimDir(self, append(append([]string{}, defaultShims...), opts.ExtraShims...))
	if err != nil {
		return 1, fmt.Errorf("wrap.Run: materialize shim dir: %w", err)
	}
	defer func() {
		// Best-effort cleanup of the whole per-session directory
		// (bin + hooks). RemoveAll handles missing dirs.
		_ = os.RemoveAll(sessionDir)
	}()

	// Per-session spawn-event socket: every shim dispatch fires one
	// fire-and-forget datagram here before exec'ing the real tool;
	// we aggregate and log per-minute rates + a session summary.
	// Fail-open — telemetry must never block the session.
	eventsSock := ""
	spawnEvents, evErr := startSpawnEventListener(sessionDir)
	if evErr != nil {
		slog.Warn("wrap: spawn-event listener unavailable; continuing without spawn telemetry", "err", evErr)
		spawnEvents = nil
	} else if spawnEvents != nil {
		eventsSock = spawnEvents.sockPath
		defer spawnEvents.stop()
	}

	bin := opts.AgentArgs[0]
	args := opts.AgentArgs[1:]

	// Open the wrap-parent's session span before building env so
	// TRACEPARENT can be injected into the child's environment.
	// Every per-call span the runtime hooks emit (via `ycode
	// internal-shell-trace`) will nest under this one, producing a
	// single tree per wrap invocation.
	telCtx, finish := telotel.StartExecSpan(ctx, telotel.ExecScopeWrappedAgent, bin, args)
	if spawnEvents != nil {
		// Exit events from span-mode shims (YCODE_WRAP_SPAWN_TRACE=1)
		// become retroactive child spans of the session span.
		spawnEvents.enableSpans(telCtx)
	}

	env := opts.Env
	if env == nil {
		env = os.Environ()
	}
	env = injectShimEnv(env, shimDir, opts)
	env = injectTraceparent(telCtx, env)
	if eventsSock != "" {
		env = append(env, spawncore.EnvEvents+"="+eventsSock)
	}

	// Runtime hooks (Phase 1.2): materialize Python sitecustomize.py
	// and/or Node ycode-trace.cjs under <shimDir>/python|node/ and
	// prepend PYTHONPATH / append NODE_OPTIONS so the wrapped agent's
	// runtime loads them at startup.
	//
	// Fail-open: any error here logs a warn and proceeds without
	// hooks — the wrap shim's value-add stays available even when
	// runtime hooks can't be installed (e.g. read-only shimDir).
	if len(opts.RuntimeHooks) > 0 {
		hooksDir := filepath.Join(sessionDir, "hooks")
		overrides, err := hookruntime.Materialize(hooksDir, opts.RuntimeHooks)
		if err != nil {
			slog.Warn("wrap: runtime hook materialize failed; proceeding without hooks",
				"langs", opts.RuntimeHooks, "err", err)
		} else {
			env = applyRuntimeOverrides(env, overrides)
		}
	}

	// PTY path: when both stdio are terminals (auto), or when the
	// user explicitly set --pty=always, run the wrapped agent under
	// a freshly-allocated PTY. SIGWINCH propagation and raw-mode
	// switching happen inside runUnderPTY; signal forwarding is not
	// needed because the controlling terminal delivers SIGINT/
	// SIGTERM/SIGHUP to the foreground PG directly.
	ptyMode := ParsePTYMode(opts.PTY)
	if shouldAllocatePTY(ptyMode, opts) {
		exitCode, err := runUnderPTY(telCtx, bin, args, env, opts.WorkDir)
		finish(exitCode, err)
		if err != nil {
			return exitCode, err
		}
		return exitCode, nil
	}

	cmd := exec.CommandContext(telCtx, bin, args...)
	cmd.Dir = opts.WorkDir
	cmd.Env = env
	cmd.Stdin = orStdin(opts.Stdin)
	cmd.Stdout = orStdout(opts.Stdout)
	cmd.Stderr = orStderr(opts.Stderr)
	// Run the foreign agent in its own process group so signal
	// forwarding can address descendants and SIGKILL escalation
	// reaches every spawned subprocess.
	cmd.SysProcAttr = newProcessGroupAttr()

	if err := cmd.Start(); err != nil {
		finish(0, err)
		return 1, fmt.Errorf("wrap.Run: start %s: %w", bin, err)
	}
	stopSignals := forwardSignalsToChild(cmd)
	err = cmd.Wait()
	stopSignals()

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	finish(exitCode, err)

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitCode, nil
		}
		return exitCode, err
	}
	return exitCode, nil
}

// IsShimInvocation reports whether the current process was started as
// a shim symlink (argv[0] is not "ycode" AND YCODE_WRAP_SHIM=1). Called
// from cmd/ycode/main.go before cobra parses, mirroring the existing
// maybeHandleGiteaHook/maybeHandleShellCmd interceptors.
func IsShimInvocation() bool {
	if os.Getenv(envShim) != "1" {
		return false
	}
	base := filepath.Base(os.Args[0])
	if base == "" || base == "." || base == "/" {
		return false
	}
	// Defense in depth: if argv[0] is literally "ycode" we're not a
	// shim even if YCODE_WRAP_SHIM somehow leaked into the env.
	return base != "ycode"
}

// ShimMain is the in-binary fallback dispatcher, used when shim
// symlinks point at the ycode binary itself: bare `go build` without
// -tags embed_spawn, ycode-spawn extraction failure, or stale shim
// dirs from sessions predating the micro shim. It delegates to the
// same spawncore.Dispatch that cmd/ycode-spawn uses: depth guard →
// PATH strip → real-binary resolve → spawn-event datagram → exec(2)
// on unix (fork-and-wait on Windows, which has no exec). Note the
// monolith's boot cost (~250ms, ~150MB) has already been paid by the
// time control reaches here — the embedded micro shim is the real
// fix; this path only keeps the fallback's semantics identical.
//
// The per-exec OTel span that used to live here was removed with the
// switch to exec(2): shims never installed a real tracer provider
// (main() routes here before any telemetry setup) so the span was a
// no-op, and after exec there is no process left to finish it. Spawn
// accounting now flows through the event datagram to the wrap
// parent, which does own a real provider.
func ShimMain() int {
	defer initLoggerFromEnv()()
	return spawncore.Dispatch(os.Args[0], os.Args[1:])
}

// injectShimEnv overlays the wrap-coordination variables on a copy of
// the base env. PATH gets the shim dir prepended; SHELL points at the
// shim's bash so subprocess shells use it. The YCODE_WRAP_* vars
// signal to ycode child invocations that they are running as a shim.
func injectShimEnv(env []string, shimDir string, opts Options) []string {
	// YCODE_BIN points the runtime hooks at the *same* ycode binary
	// the wrap parent is running, not at whatever `ycode` happens to
	// resolve first on PATH (which is often a stale installed copy).
	// The hooks honor it before falling back to PATH lookup. Caller-
	// provided YCODE_BIN (e.g. the e2e test's tap script) wins: tests
	// route the trace subprocess through a recorder, and wrap must
	// not stomp that.
	overrides := map[string]string{
		"PATH":                   shimDir + string(os.PathListSeparator) + extractEnv(env, "PATH"),
		"SHELL":                  filepath.Join(shimDir, "bash"),
		envShim:                  "1",
		envDepth:                 "0",
		envShimDir:               shimDir,
		envWrappedAgent:          filepath.Base(opts.AgentArgs[0]),
		"YCODE_WRAP_OTEL_EXPORT": string(ParseExportMode(opts.OTelExport)),
		"YCODE_WRAP_PROFILE":     resolvedProfileName(&opts),
	}
	if extractEnv(env, "YCODE_BIN") == "" {
		if selfBin, err := os.Executable(); err == nil {
			overrides["YCODE_BIN"] = selfBin
		}
	}
	out := make([]string, 0, len(env)+len(overrides))
	seen := make(map[string]bool, len(overrides))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		key := kv[:eq]
		if v, ok := overrides[key]; ok {
			out = append(out, key+"="+v)
			seen[key] = true
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		if !seen[k] {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// stripShimFromPath returns PATH with shimDir removed. Used by the
// child before LookPath so the real binary doesn't resolve back to the
// shim symlink. Operates on an absolute shimDir.
func stripShimFromPath(path, shimDir string) string {
	if shimDir == "" {
		return path
	}
	parts := strings.Split(path, string(os.PathListSeparator))
	out := parts[:0]
	for _, p := range parts {
		if p == shimDir {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func extractEnv(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}

func orStdin(r io.Reader) io.Reader {
	if r == nil {
		return os.Stdin
	}
	return r
}

func orStdout(w io.Writer) io.Writer {
	if w == nil {
		return os.Stdout
	}
	return w
}

func orStderr(w io.Writer) io.Writer {
	if w == nil {
		return os.Stderr
	}
	return w
}

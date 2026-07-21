# Agent OS — `ycode wrap`

> For the framing-level reference (what an Agent OS is in 2026 and where ycode stands against the SOTA bar), see [`agent-os-reference.md`](./agent-os-reference.md). This file documents one axis of that framing.

`ycode wrap` is the involuntary-interception axis of ycode's Agent OS framing. Foreign agentic CLIs (Claude Code, opencode, Aider, Gemini CLI, Codex) launched through it inherit ycode's observability and best-effort policy *whether the agent knows about ycode or not* — complementing the voluntary [lighthouse beam](./lighthouse.md) — the `yc <verb>` shell built-ins — for agents that opt in by routing their bash through `ycode shell`.

> **Honest scope:** Ring 1 (PATH shim + runtime hooks) is **observability + best-effort policy, not a security boundary**. Hardened sandboxing on Linux (Landlock + seccomp_unotify) lands in a separate plan. macOS gets best-effort `sandbox-exec`; Windows is telemetry-only.

## Quick start

```bash
# Wrap a known agent — auto-detects the profile from argv[0] basename.
bin/ycode wrap claude
bin/ycode wrap opencode
bin/ycode wrap aider

# Headless tasks (Claude Code's print mode).
bin/ycode wrap claude -p "explain this repo's package layout"

# Override the runtime-hook policy.
bin/ycode wrap --runtime-hooks=off opencode
bin/ycode wrap --runtime-hooks=python,node aider

# Send spans to a different sink.
bin/ycode wrap --otel-export=console opencode   # JSON span dump to stderr
bin/ycode wrap --otel-export=off opencode        # no provider (slog debug only)
```

## What it does

Three layers of interception, each independently observable in pulse:

1. **PATH shim** — every command the wrapped agent looks up on `$PATH` (`bash`, `rg`, `git`, `jq`, `sed`, `awk`, `python`, `node`, ...) is re-routed through `ycode`, which exec's the real binary inside an OTel span tagged `ExecScopeWrappedAgent`.
2. **Language runtime hooks** (Python, Node) — `sitecustomize.py` and a Node `--require` module patch `subprocess`/`child_process` so even shell-form (`shell=True`) and absolute-path invocations get parsed (via ycode's bash AST), validated (V01–V12), and traced.
3. **Foreign-agent attribution** — every emitted span carries `wrap.agent` and `wrap.profile` attributes so pulse dashboards can slice by agent identity.

## Supported agents

The wrap shim and PATH-level interception work for **all** agents. The runtime-hook layer (which closes the absolute-path bypass) coverage varies by agent runtime:

| Agent | Profile | PATH shim | Runtime hook | Notes |
|---|---|---|---|---|
| **opencode** | `opencode` | yes | yes (Node) | Full coverage. Node `child_process.*` interceptors fire on `shell=true` and exec-form. |
| **Aider** | `aider` | yes | yes (Python) | Full coverage. `subprocess.Popen.__init__` patched at sitecustomize load. |
| **Gemini CLI** | `gemini` | yes | yes (Node) | Should work like opencode; not yet validated end-to-end. |
| **Claude Code** | `claude` | yes | partial | The `claude` binary is a single Bun-compiled Mach-O executable that **does not honor `NODE_OPTIONS=--require`**. The Node hook does not load inside Claude's process. PATH-level shim still catches everything Claude shells out to via `$PATH`; absolute-path calls inside Claude bypass tracing. |
| **Codex** | `codex` | yes | no (Rust) | Rust runtime has no language-level hook. Only the PATH shim catches what Codex routes through `$PATH`. A one-line stderr warn surfaces this at wrap-start. |

## Telemetry

The wrap parent installs an OTel provider configured by `--otel-export`:

- `file` (default) — spans land in `~/.agents/ycode/otel/instances/wrap-<uuid>/traces/traces-YYYY-MM-DD.jsonl`. One instance dir per wrap session; trace subprocesses (`ycode internal-shell-trace`) get their own instance dirs and nest under the wrap parent's span via W3C `TRACEPARENT` propagation.
- `console` — same as `file`, plus a stdouttrace processor that pretty-prints every span to stderr as JSON.
- `off` — no provider installed; spans surface only via `slog.Debug` log lines when `YCODE_LOG_LEVEL=debug`.

When `ycode serve` is running (advertising an OTLP endpoint in `~/.agents/ycode/manifest.json`), the wrap parent **also** pushes spans to that collector, so dashboards mounted under `ycode serve` see real-time wrap traffic.

Override the local mode with `YCODE_WRAP_OTEL_EXPORT=off|file|console` for one-off debugging.

## PTY behavior

Interactive TUIs (Claude Code's REPL, opencode's session) need a controlling terminal. `--pty` controls allocation:

- `auto` (default) — allocate a PTY when both stdin and stdout are terminals; otherwise inherit FDs unchanged. Right for both `ycode wrap claude` (TUI) and `ycode wrap claude -p "task" < input.txt` (headless pipeline).
- `always` — force PTY even when stdin is piped. Useful for TUIs that test for TTY-ness internally.
- `never` — inherit-FD always.

PTY mode forwards `SIGWINCH` (terminal resize) into the wrapped agent and switches the host terminal to raw mode for the duration.

## Runtime-hook policy

The Python sitecustomize and Node `--require` modules install **only when the resolved profile lists them in `RuntimeHooks`**. Unknown agents (`ycode wrap /tmp/random.sh`) skip the hooks by default — predictable, no collisions with agents that ship their own subprocess customization.

Override surface:

- CLI: `--runtime-hooks=auto|off|<lang,lang>`. `auto` follows the profile; `off` disables for the session; a comma list (`python,node`) installs those exact hooks regardless of profile.
- Env: `YCODE_WRAP_RUNTIME_HOOKS=off` (or any of the above values).

**Failure mode:** the hooks **fail open**. Any error inside the trace path (subprocess crashes, timeout, JSON-decode failure) is logged to stderr with a `[ycode wrap hook]` prefix; the original `subprocess.run` / `child_process.spawn` call proceeds unmodified. Telemetry is best-effort; the wrapped agent is never broken by a ycode trace bug.

## Codex limitation

`ycode wrap codex` emits a one-line stderr warn at startup:

```
[ycode wrap] codex: Rust runtime — no language-level hook; shell-outs via absolute paths bypass tracing. PATH-shim coverage only.
```

Codex stays in the profile registry so its shim catalog (extra entries for `node`, `npx`, `python3`) still applies — the user just doesn't get the bypass-closing runtime hook that Python and Node agents enjoy. Tracing Rust-binary subprocess calls would require eBPF/LD_PRELOAD/Endpoint Security — explicitly out of scope for Phase 1.3.

## Claude Code limitation

The `claude` binary is a Bun-compiled standalone executable that does not load `NODE_OPTIONS=--require` modules. Spans are still emitted for everything Claude routes through `$PATH` (its `Bash` tool invocations all hit `bash` → caught by the shim), but absolute-path subprocess calls inside Claude's bundled JavaScript runtime bypass the trace.

Workaround for full coverage: run Claude Code inside the wrap interactively so all its bash-tool invocations flow through the PATH shim; the PATH shim catches `bash` regardless of how Claude invokes it.

## Verifying spans land

After a wrap session, inspect the latest instance dir:

```bash
LATEST=$(ls -1dt ~/.agents/ycode/otel/instances/wrap-* | head -1)
ls "$LATEST/traces/"
# Each line is one span with name, attributes, parent ID, etc.
jq '. | {name: .Name, agent: ([.Attributes[] | select(.Key=="wrap.agent")] | first | .Value.Value)}' \
   "$LATEST/traces/traces-$(date -u +%F).jsonl"
```

Spans tagged `ycode.exec.wrapped-agent` are the parent (per-shell-out or per-wrap-session); `ycode.exec.wrapped-agent.cmd` are children emitted by the runtime hooks for each parsed `CommandNode`.

## Smoke matrix

`scripts/wrap-smoke.sh` runs all known profiles against deterministic fixtures plus real-agent `--help` / `--version` / Claude Code `-p` rows. Skips agents not on PATH:

```bash
make compile  # build bin/ycode first
RESULTS_DIR=/tmp/wrap-smoke bash scripts/wrap-smoke.sh
cat /tmp/wrap-smoke/wrap-smoke.md
```

The matrix surfaces per-row span counts. Fixture rows (2 spans each — wrap parent + one shim invocation) act as the regression gate; real-agent rows are best-effort and depend on what the installed agent actually shells out to during `--help`.

Interactive (TUI) testing is a manual checklist in the script header — the e2e suite covers headless paths only.

# `ycode shell`

An interactive shell that is **bash-compatible at the command layer** and
**LLM-mediated at the UX layer**, powered by the embedded Ollama runner.
Bare words behave exactly like `/bin/bash`. Agentic features sit above
bash via *sentinel prefixes*; they never override bash's first-word
resolution.

```bash
ycode shell                 # Bubble Tea TUI (default)
ycode shell --no-tui        # plain stdin/stdout REPL — required for vi/less/top
ycode shell --workdir /tmp  # initial working directory
```

## Surface conventions

For the first non-whitespace token of a logical line:

| Form | Meaning |
|---|---|
| `<bare word>` | Bash dispatch (reserved → builtin → function/alias → PATH) |
| `/<word>` | Slash command from the curated shell-safe registry |
| `/path/with/slashes` | Filesystem path, normal bash |
| `@<id>` | Skill from registry |
| `@<path>` | Skill loaded from filesystem path |
| `!<text>` | One-shot agent with shell context (stub in skeleton) |
| `?<text>` | Cheap LLM Q&A, no tools (stub in skeleton) |

Sentinels apply **only** at the first non-whitespace token of a logical
line. Inside arguments, quotes, heredocs, command-substitution, mid-line,
or pipelines they are literal text. PATH always wins for bare words —
nobody can ship a skill named `ls` and break muscle memory.

### Pipe-to-sentinel: `<bash> | @<sentinel>`

The skeleton supports piping bash output **into** an `@`-sentinel:

```bash
ls -la | @summarize
git log --oneline | head -20 | @explain how many commits
cat /etc/passwd | @./skills/triage
```

The upstream bash runs first; its captured stdout is fed to the skill as
input alongside any explicit args. Slash commands (`/`) never accept
piped input. Sentinels at the *start* of a pipeline (`/help | grep foo`)
remain disallowed (`ErrSentinelInPipeline`).

### Quoting always wins

```bash
"echo /help"        # bash — runs the literal command "echo /help"
echo "@something"   # bash — prints @something
git commit -m "/x"  # bash — sentinel only fires at first token
```

## Curated shell-safe slash commands

To avoid panics from handlers that depend on the agent REPL's `*App`
context, only these slash commands are wired in shell mode:

- `/help` — sentinel reference + this doc
- `/version` — ycode build version
- `/clear` — clear the screen

`/init`, `/commit`, `/model` and other agent-REPL handlers report
*"not available in shell mode — use the ycode REPL"* when invoked.
Lifting this restriction is a separate refactor (introduce
`commands.Context` so handlers can run without `*App`).

## Permission posture

`ycode shell` runs at `permission.DangerFullAccess` by default — the
user is the operator, same posture as `/bin/bash` itself. The agent-mode
validators (V01–V12 in `internal/runtime/bash/validators.go`) are **not**
applied in shell mode; they are agent policy, not user policy. Override
via `--permission read-only` / `--permission workspace-write` for
sandboxed shell sessions.

## What persists across submissions

The persistent mvdan/sh runner keeps these alive between submissions:

- Environment variables (`export FOO=bar`, then `echo $FOO` works)
- Shell variables and arrays (`A=(1 2 3)`)
- Functions (`greet() { echo hi; }`)
- Working directory (`cd /tmp` is sticky)
- Shell options: `set -e`, `set -u`, `set -o pipefail`
- Aliases (`shopt -s expand_aliases; alias g='echo greeted'`)

**Known skeleton limitations** (tracked outside skeleton scope):

- Multi-stage pipelines mixing both forms (`cmd | @sk | grep foo`) are
  not yet supported. Trailing `<bash> | @sentinel` (skeleton §13b) and
  leading `@sentinel | <bash>` (F4) both work.

## Tab completion

Tab is namespace-aware:

- `/` + Tab → curated shell-safe slash commands
- `@` + Tab → skill registry
- bare word + Tab → first PATH binaries matching the prefix (capped to
  32 per directory for latency)

Single match → autocomplete inline. Multiple matches → list dumped into
the viewport, input untouched.

## Signals

| Key | Idle prompt | Command running |
|---|---|---|
| `^C` | Discard current input | Cancel running command (SIGTERM→SIGKILL escalation to child pgid) |
| `^D` | Exit shell (empty input only) | Ignored |

`exit` runs through bash and exits the runner.

## Relationship to the `bash` agent tool

`internal/runtime/bash` ships two surfaces:

1. **Agent mode** (`InterpreterExecutor` + the `bash` tool registered in
   `internal/tools/bash.go`): fresh runner per call, validators V01–V12,
   no streaming. Used by Codex/Claude Code/OpenCode-style agents and by
   the HTTP API under `ycode serve`. Untouched by the shell work.
2. **Shell mode** (`(*ShellSession).RunString` + `NewShellExecHandler`):
   long-lived runner, no validators, optional `TTYRunner` for PTY
   handoff. Used by `ycode shell` only.

Both share the `ShellSession` struct (cwd tracking) so a future Web UI
or remote shell could mix and match.

## Verification

```bash
make compile                                    # build the binary
go test -short -race ./internal/shell/...       # unit tests
go test -short -race ./internal/runtime/bash/...# regression — agent path

# Smoke
bin/ycode shell --no-tui --workdir /tmp <<'EOF'
echo hello
FOO=bar
echo $FOO
greet() { echo "hi $1"; }
greet world
cd /tmp && pwd
/help
/version
@./does-not-exist
ls -la | @summarize
"echo /help"
exit
EOF

# E2E (requires bin/ycode pre-built)
go test -tags e2e -timeout 60s -count=1 ./internal/shell/...
```

## What's NOT in the skeleton

These intentionally land later, not now:

- LLM-mediated input classifier (`!`/`?` are stubs).
- Smart history (semantic recall via the episodic memory store).
- LLM-powered programmable completion.
- Fuzzy correction (`ls-al` → `ls -al`).
- `permission.InteractiveShell` mode (the user picked `DangerFullAccess`
  for the skeleton — see plan §13f decision 3).
- TTY commands inside the Bubble Tea TUI (use `--no-tui` for vi/less/top).
- Multi-line input editor (single-line submit only; heredocs work).

See plan §13d for the full non-goal list and §13f for resolved open
questions.

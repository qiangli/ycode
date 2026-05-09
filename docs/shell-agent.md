# `ycode shell` for foreign agents

ycode shell is designed to be the **executor** for any agentic coding tool — Claude Code, OpenCode, Codex, Continue, Cursor, an older ycode build. Agents that point their `bash` tool at `ycode shell -c` get ycode's killer capabilities (treesitter AST, repo map, code graph, sandbox, browser-use, semantic memory, native git) as plain shell commands with **no MCP setup required**.

The framing: **bash compatibility unbroken; ycode capabilities surfaced as `yc <verb>` built-ins; agent-friendly output augmentation behind a flag.**

## Quick start

```bash
# Capability discovery (read once at agent startup)
ycode shell --manifest

# One-shot execution (matches bash -c)
ycode shell -c "ls -la"

# Agent posture: auto-quiet, hints to stderr, env var set
ycode shell --agent -c "grep -r FooBar internal/"
# stderr → # ycode hint [code-search]: try `yc search-symbols 'FooBar'` …

# Hints only (no execution) — for agents that want to consult before acting
ycode shell --suggest "tree internal/"

# Containerized execution (Phase B; podman required)
ycode shell --agent --sandbox -c "..."
```

## The recommended trio for production

```
ycode shell --agent --sandbox --json
```

Each flag is independent and orthogonal:

- **`--agent`** turns on output augmentation, sets `YCODE_SHELL_AGENT=1`, implies `--quiet`.
- **`--sandbox`** routes external commands through podman (Phase B).
- **`--json`** wraps each result as a JSON envelope (Phase B).

## Built-in commands (`yc <verb>`)

These intercept *before* `$PATH` lookup so they're always available, never shadowed, and call into ycode's internal Go packages directly (no fork, no MCP round-trip).

| Verb | Purpose | Beats |
|---|---|---|
| `yc symbols <path>` | List top-level symbols | grep, ctags |
| `yc search-symbols <pattern>` | AST-aware symbol search | grep -r |
| `yc refs <symbol>` | Find references and callers | grep + manual analysis |
| `yc repomap [--budget=N]` | Token-budgeted file→symbol overview | find ... \| xargs head |
| `yc help` | List all built-ins | — |
| `yc manifest` | JSON capability dump | — |

Phase B will add: `yc graph "<DQL>"`, `yc git <subcmd>`, `yc browser open|fetch|find`, `yc sandbox -- <cmd>`, `yc remember`/`yc recall`.

## Agent-mode hints (the teaching layer)

When `--agent` is on, ycode appends hints to **stderr** (or to a `hints[]` array in `--json` mode) when the bash command matches a pattern where a `yc <verb>` would have been better. Stdout is never touched — pipes stay clean.

Initial pattern catalog (regex-driven, expand via `internal/shell/agentmode/hints.go`):

| Pattern | Hint |
|---|---|
| `grep -r` / `rg` / `ack` | `yc search-symbols` (AST-aware) |
| `find ... -name *.go` | `yc symbols` / `yc repomap` |
| `tree` / `ls -R` | `yc repomap` *or* `yc graph "<DQL>"` |
| `wc -l *.go` | `yc symbols` |
| `curl https?://` | `yc browser fetch\|open` |
| `git log\|status\|diff\|...` | `yc git $1` (native go-git) |
| `cat ... \| head` | `yc repomap --budget=N` |
| `rm -rf` | (advisory) `--sandbox` for copy-on-write |
| exit 127 | `yc help` to discover built-ins |
| permission denied | `--sandbox` for podman isolation |

Within a single process, hints dedup by ID (you don't get spammed with the same tip on every command). Cross-process dedup is Phase C work.

## Integration recipes

### Claude Code

Claude Code does **not** expose a settings.json key to swap the `Bash`
tool's underlying shell binary. The integration paths that work today,
in order of cleanliness:

#### Path 1 — MCP (recommended)

Add to `~/.claude/settings.json` or `.claude/settings.json`:

```json
{
  "mcpServers": {
    "ycode-shell": {
      "command": "ycode",
      "args": ["mcp", "serve"]
    }
  }
}
```

`ycode mcp serve` exposes the `agent_shell` tool (plus the existing
treesitter handler). The default permission ceiling is
`danger-full-access` — agents that intentionally configured
`ycode mcp serve` have opted into ycode's full surface. Lower with
`--permission=read-only` or `--permission=workspace-write` for
sandboxed integrations.

#### Path 2 — PreToolUse hook that rewrites Bash commands

`PreToolUse` hooks can mutate `updatedInput.command` before Claude's
Bash tool runs the call. A hook can prepend `ycode shell --agent --json -c`
to every Bash command, so all Claude shell traffic flows through the
agentic surface without the agent knowing.

Sketch (in `~/.claude/settings.json`):

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "Bash",
      "hooks": [{
        "type": "command",
        "command": "jq '. + {\"updatedInput\": {\"command\": (\"ycode shell --agent -c \" + (.toolInput.command | @sh))}}'"
      }]
    }]
  }
}
```

This is invasive and finicky — debug carefully. Recommended only when
you want global agent-mode coverage and can't use MCP.

#### Path 3 — PATH wrapper (per-session, recommended for full bash interception)

Inspection of `reference/open-claude-code/v2/src/tools/bash.mjs`
(Claude Code clone) shows the Bash tool is implemented as:

```js
spawn('bash', ['-c', input.command], { env: { ...process.env } })
```

Node's `child_process.spawn` resolves bare names like `'bash'` via the
calling process's `$PATH`. This means a PATH-scoped wrapper is the
clean interception point — **scoped to a single Claude Code session,
not the whole system**:

```bash
mkdir -p ~/bin/ycode-wrappers
cat > ~/bin/ycode-wrappers/bash <<'EOF'
#!/bin/sh
# Claude Code invokes: bash -c "<command>"
# Route that through ycode shell --agent.
if [ "$1" = "-c" ] && [ $# -ge 2 ]; then
    exec ycode shell --agent -c "$2"
fi
exec /bin/bash "$@"
EOF
chmod +x ~/bin/ycode-wrappers/bash

# Only this Claude session sees the wrapper:
PATH="$HOME/bin/ycode-wrappers:$PATH" claude
```

Verify the wrapper is being called by Claude before swapping to the
ycode-shell variant:

```sh
#!/bin/sh
echo "[wrapper saw: $@]" >> /tmp/claude-bash-trace.log
exec /bin/bash "$@"
```

If `/tmp/claude-bash-trace.log` fills up when Claude runs commands →
production Claude Code uses the same `spawn('bash', ...)` form as the
clone, and the ycode-shell wrapper will work.

If the trace log stays empty, Claude Code is calling something else
(`/bin/sh -c`, `$SHELL`, a hardcoded path) — fall back to Path 1 (MCP)
or Path 2 (PreToolUse hook).

#### Path 4 — Tell Claude in the prompt

The simplest path for ad-hoc use: tell Claude to use `ycode shell -c`
explicitly. Claude's existing Bash tool runs whatever you ask:

> "Use `ycode shell --agent --json -c \"yc symbols ./src\"` to enumerate symbols."

### OpenCode

Two paths:

1. **MCP** — point OpenCode's MCP config at `ycode mcp serve` (already shipped per `docs/lighthouse.md`). The MCP server can advertise `yc *` built-ins as discrete tools so the agent invokes them without going through bash at all.
2. **Bash override** — same recipe as Claude Code, point the bash tool at `ycode shell --agent -c`.

### Codex

Codex's exec layer is configurable via the agent's tool definition. Same recipe: point the bash-equivalent at `ycode shell --agent -c`. Use `--json` for structured parsing.

### Self / older ycode builds

`bin/ycode mcp serve` (Phase 0 per `docs/lighthouse.md`) gains an `agent_shell` tool in Phase B6 whose handler internally calls the same dispatcher. Same code, three transport flavors (stdio MCP, SSE MCP, direct CLI).

## Permission posture

`ycode shell -c` defaults to `permission.DangerFullAccess` — same posture as `/bin/bash`. Surprising agents with restricted defaults breaks their existing scripts. Recommendation: combine `--agent` with `--sandbox` (Phase B) for safety.

Override per-invocation:

```bash
ycode shell -c --permission read-only "ls /etc"           # blocks writes
ycode shell -c --permission workspace-write "build.sh"    # writes only inside cwd
ycode shell -c --sandbox "rm -rf experiment/"             # podman-isolated
```

## Capability discovery

Agents read the manifest once at startup:

```bash
ycode shell --manifest
```

Returns a JSON object with: `version`, `sentinels`, `permission_modes`, `modes`, `builtins`, `skills`, `slash_commands`, and (when agentmode is loaded) `hints`. Foreign agents that prefer to do their own routing read this and skip the round-trip per command.

## Bash compatibility guarantee

ycode shell is bash-compatible at the command layer. If an agent's existing script runs under `bash -c "…"`, it runs under `ycode shell -c "…"`. The differences are *additive*:

- `yc <verb>` is a new namespace; bash never used it.
- Sentinels (`/`, `@`, `!`, `?`) only fire as the first non-whitespace token of a logical line. `git commit -m "/foo"` is plain bash.
- Pipelines, heredocs, redirections, env vars, functions, `set -e` / `set -u` / `set -o pipefail`, aliases all work and persist across submissions in the persistent runner.
- Mid-pipeline sentinels: trailing `<bash> | @<sentinel>` and leading `@<sentinel> | <bash>` work; multi-stage `cmd | @sk | grep` is bash-literal in v1.

## Verification

```bash
# Built-ins are reachable from inside the shell
ycode shell -c 'yc help'
ycode shell -c 'yc symbols ./internal/shell/sentinel.go'
ycode shell -c 'yc repomap --budget=2000 internal/shell'

# Agent posture works
ycode shell --agent -c 'grep -r FooBar' 2>&1 | grep '# ycode hint'

# Manifest loads
ycode shell --manifest | jq '.builtins | length'
```

## What is NOT in scope

- ycode shell does NOT impose ycode's permission validators (V01–V12) on agents — that's agent-mode bash-tool policy. Shell mode trusts the operator.
- ycode shell does NOT replace `/bin/bash` system-wide. It's an agent executor and an interactive agentic shell, not a login shell replacement.
- LLM-mediated suggestions for novel commands (where the regex catalog doesn't match) are Phase C work behind `--agent=smart`.

## See also

- [docs/shell.md](./shell.md) — the human-facing interactive shell
- [docs/lighthouse.md](./lighthouse.md) — exposing ycode capabilities to foreign agents via MCP
- [internal/shell/agentmode/hints.go](../internal/shell/agentmode/hints.go) — the hint catalog (one-line additions)
- [internal/shell/builtins/](../internal/shell/builtins/) — the `yc <verb>` registry

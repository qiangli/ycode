# Claude Code → ycode integration

[Claude Code](https://docs.claude.com/en/docs/claude-code) reaches ycode
through its **bash tool**, not through MCP. Point Claude Code's shell at
`ycode shell` and ycode's treesitter, repomap, code-graph, memory, and
native-git capabilities resolve in-process as `yc <verb>` built-ins —
no server, no daemon, no auth.

> ycode runs **no MCP server**. Earlier revisions of this doc told you to
> paste a `ycode` entry into `.mcp.json` or `~/.claude/settings.json`.
> That endpoint no longer exists; a stale entry makes Claude Code report
> a failed MCP server on every launch. Remove it. See
> [plan-remove-mcp.md](./plan-remove-mcp.md).

For the cross-tool guide see
[integration-foreign-agents.md](./integration-foreign-agents.md). For
opencode see [integration-opencode.md](./integration-opencode.md).

## Steps

### 1. Make the repo ycode-aware

```bash
ycode init
```

Writes `<repo>/.ycode/AGENTS.md` and splices a delimited capability block
into `<repo>/CLAUDE.md` (and `~/.claude/CLAUDE.md` at user scope) so
Claude's LLM knows the `yc` verbs exist. Details in
[selfinit.md](./selfinit.md).

### 2. Install the PATH wrapper

Claude Code's Bash tool spawns a bare `bash`, which Node resolves through
the calling process's `$PATH`. That makes a PATH-scoped wrapper the clean
interception point — scoped to one Claude Code session, not the system:

```bash
mkdir -p ~/bin/ycode-wrappers
printf '#!/usr/bin/env -S ycode shell --agent\n' > ~/bin/ycode-wrappers/bash
chmod +x ~/bin/ycode-wrappers/bash
ln -sf bash ~/bin/ycode-wrappers/zsh
```

The wrapper is a one-line shebang: ycode *is* bash (its `shell`
subcommand speaks bash via `mvdan/sh` and adds the `yc <verb>`
built-ins). `env -S` splits the shebang args on Linux; macOS splits
natively.

### 3. Launch Claude Code with the wrapper in front

```bash
PATH="$HOME/bin/ycode-wrappers:$PATH" claude
```

Every `Bash` tool call now runs through ycode's agent-mode shell: the
`yc` verbs are available, and the hint engine writes suggestions to
stderr when a plain command (`grep -rn`, `find -name`) has a better `yc`
answer.

## What Claude gets

| Capability | Command |
|---|---|
| Declarations in a file or dir | `yc symbols <path>` |
| Workspace symbol search | `yc search-symbols <pattern> [path]` |
| Callers / references | `yc refs <symbol>` |
| Repo orientation | `yc repomap [--budget=N] [--query=…]` |
| Code knowledge graph (DQL) | `yc graph "<query>"` |
| Pure-Go git | `yc git <log\|status\|diff\|branch\|show\|blame>` |
| Semantic memory | `yc remember "<fact>"` / `yc recall <query>` |
| Structured test/LSP/exec output | `yc test --json`, `yc lsp <action> --json`, `yc run --json -- <cmd>` |
| Capability discovery | `yc help`, `yc manifest`, `ycode docs <topic>` |

These are Go functions dispatched before `$PATH` lookup — they cannot be
shadowed, and they cost a function call rather than a subprocess.

### Why route Bash through ycode rather than leave it alone

- The LLM gets the `yc` built-ins without a tool-list change.
- Pre/post-exec hints from ycode's agent-mode catalog fire on stderr
  (e.g. suggesting `yc search-symbols` when the agent ran `grep -r`).
- Telemetry flows through ycode's pulse stack.

For plain bash the behavior is identical to `/bin/bash`; the wrapper is
not a sandbox.

## Alternative paths

If the PATH wrapper doesn't take (some builds exec `/bin/sh` or a
hardcoded shell), two fallbacks:

- **`PreToolUse` hook** — rewrite `updatedInput.command` to prepend
  `ycode shell --agent -c`. Invasive and finicky; recipe in
  [shell-agent.md](./shell-agent.md).
- **Ask in the prompt** — "Use `ycode shell --agent --json -c \"yc symbols
  ./src\"`". Claude's existing Bash tool runs whatever you ask. Fine for
  ad-hoc use, useless for consistency.

Verify which case you're in with a logging wrapper:

```sh
#!/bin/bash
echo "[wrapper saw: $@]" >> /tmp/claude-bash-trace.log
exec /bin/bash "$@"
```

If the log fills when Claude runs commands, the ycode wrapper will work.

## Permission modes

Claude Code's own permission modes gate the `Bash` tool before anything
reaches ycode: `bypassPermissions` allows it silently, `acceptEdits` and
`default` prompt, `plan` blocks it outright. In `plan` mode the `yc`
verbs are unreachable along with everything else bash-shaped.

Underneath, `ycode shell -c` defaults to `DangerFullAccess` — the same
posture as `/bin/bash`. Tighten it per invocation:

```bash
ycode shell -c --permission read-only "ls /etc"
ycode shell -c --permission workspace-write "./build.sh"
```

## Parallel sub-agents

Claude Code dispatches sub-agents via its `Agent` tool, which share one
working tree. To give each sub-agent an isolated git workspace, use the
sibling AgentOS command `bashy weave` — ycode's own loom substrate was
MCP-only and no longer exists. Start with `bashy weave guide`.

## Telemetry: Claude Code → pulse

Claude Code honors standard OTLP env vars. Point it at ycode's
collector:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
export OTEL_SERVICE_NAME=claude-code
```

Claude Code's spans/metrics/logs land in pulse alongside ycode's own and
any other paired client. This is independent of the shell integration —
it works whether or not the wrapper is installed.

## Common gotchas

- **A `ycode` entry in `mcpServers` breaks startup.** ycode serves no
  MCP endpoint. Delete the entry from `.mcp.json` and
  `~/.claude/settings.json`.
- **`yc: command not found`.** The wrapper isn't in `$PATH` for that
  session, or Claude isn't spawning a bare `bash`. Run the trace check
  above.
- **Wrapper is global when you didn't want it.** Set `PATH` inline on
  the `claude` invocation rather than in `~/.zshrc`.
- **`ycode serve` is not required.** The shell path is entirely local.
  `serve` only matters for the HTTP API and telemetry.

## Verification (smoke test)

With the wrapper active, in a Claude Code session:

> "Run `yc symbols ./internal/docs` and show me the output."

Expected: Claude calls its `Bash` tool; the output is a treesitter symbol
listing, not `command not found`.

Then:

> "Run `yc manifest` and tell me which built-ins are available."

Expected: a JSON capability catalog naming `symbols`, `search-symbols`,
`refs`, `repomap`, `graph`, `git`, `remember`, `recall`, `test`, `lsp`,
`run`.

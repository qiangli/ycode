# opencode → ycode integration

[opencode](https://github.com/sst/opencode) reaches ycode through its
**shell tool**, not through MCP. Point opencode's bash backend at
`ycode shell` and ycode's treesitter, repomap, code-graph, memory, and
native-git capabilities resolve in-process as `yc <verb>` built-ins —
no server, no daemon, no auth.

> ycode runs **no MCP server**. Earlier revisions of this doc told you to
> paste a `ycode` remote into `~/.opencode/opencode.jsonc`'s `mcp` block.
> That endpoint no longer exists; a stale entry makes opencode report a
> failed MCP server on every launch. Remove it. See
> [plan-remove-mcp.md](./plan-remove-mcp.md).

For the cross-tool guide (claude code, codex, gemini-cli) see
[integration-foreign-agents.md](./integration-foreign-agents.md).

## Steps

### 1. Make the repo ycode-aware

```bash
ycode init
```

Writes `<repo>/.ycode/AGENTS.md`, splices a delimited capability block
into `<repo>/AGENTS.md`, and refreshes opencode's user-scope memory file
at `~/.config/opencode/AGENTS.md` so opencode's LLM knows the `yc` verbs
exist. No opencode config file is touched. Details in
[selfinit.md](./selfinit.md).

### 2. Install the PATH wrapper

opencode's shell tool spawns a bare `bash`, resolved through `$PATH`:

```bash
mkdir -p ~/bin/ycode-wrappers
printf '#!/usr/bin/env -S ycode shell --agent\n' > ~/bin/ycode-wrappers/bash
chmod +x ~/bin/ycode-wrappers/bash
ln -sf bash ~/bin/ycode-wrappers/zsh
```

### 3. Launch opencode with the wrapper in front

```bash
PATH="$HOME/bin/ycode-wrappers:$PATH" opencode
```

Every shell call now runs through ycode's agent-mode shell, with the
`yc` built-ins available and the hint engine writing suggestions to
stderr.

## What opencode gets

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

These dispatch before `$PATH` lookup, so nothing on the host can shadow
them, and they cost a function call rather than a subprocess.

### Why route the shell tool through ycode

- The LLM gets the `yc` built-ins without any tool-list change — no new
  tool descriptions to compete with opencode's own.
- Pre/post-exec hints from ycode's agent-mode catalog fire on stderr
  (e.g. suggesting `yc search-symbols` when the agent ran `grep -r`).
- Telemetry flows through ycode's pulse stack.

For plain bash the behavior is identical to `/bin/bash`.

## Per-agent expectations

opencode's `Agent` system gates tool calls per-agent, and it gates the
*shell tool* — so the `yc` verbs inherit exactly that policy:

- **plan agent** — read-only; bash denied, therefore no `yc` verbs.
- **build agent** — full access; every `yc` verb available.
- **scout agent** — read-only with relaxed rules; bash denied.

Denials happen on opencode's side; nothing reaches ycode. Underneath,
`ycode shell -c` defaults to `DangerFullAccess` (same posture as
`/bin/bash`); tighten per invocation with
`--permission read-only` or `--permission workspace-write`.

## Parallel sub-agents

opencode ships its own `Worktree` service for sub-agent isolation — use
it. ycode's loom substrate was MCP-only and no longer exists. For
cross-tool fan-out with verified convergence, the sibling AgentOS
command is `bashy weave` (`bashy weave guide`).

## Telemetry: opencode → pulse

opencode honors standard OTLP env vars. Point it at ycode's collector:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
export OTEL_SERVICE_NAME=opencode
```

opencode's spans/metrics/logs land in pulse alongside ycode's own and any
other client. Independent of the shell integration — this works whether
or not the wrapper is installed. Also covered in
[integration-foreign-agents.md](./integration-foreign-agents.md).

## Common gotchas

- **A `ycode` entry in the `mcp` block breaks startup.** ycode serves no
  MCP endpoint. Delete it from `~/.opencode/opencode.jsonc` and any
  project `.opencode/opencode.jsonc`.
- **bash denied in plan mode.** opencode's `plan` agent denies
  write-class tools, including the shell. Switch to the `build` agent.
- **`yc: command not found`.** The wrapper isn't in `$PATH` for that
  session. Confirm with `which bash` from inside an opencode shell call.
- **`ycode serve` is not required.** The shell path is entirely local.

## Verification (smoke test)

With the wrapper active, ask opencode (in build mode):

> "Run `yc symbols ./internal/docs` and show me the output."

Expected: a treesitter symbol listing, not `command not found`.

Then:

> "Run `yc manifest` and tell me which built-ins are available."

Expected: a JSON capability catalog naming `symbols`, `search-symbols`,
`refs`, `repomap`, `graph`, `git`, `remember`, `recall`, `test`, `lsp`,
`run`.

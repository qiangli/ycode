# opencode → ycode integration

[opencode](https://github.com/sst/opencode) speaks MCP natively. Pointing
it at a running `ycode serve` exposes ycode's bash, treesitter, skills,
gitea, loom, and pulse families as `ycode_*` tools that opencode's LLM can
call directly. No plugins, no second MCP entry, no `ycode mcp serve`
subprocess to manage.

This is the canonical recipe for the `ycode pair --tool opencode` flow.
For the cross-tool guide (claude code, codex, gemini-cli) see
[integration-foreign-agents.md](./integration-foreign-agents.md).

## Steps

### 1. Start ycode serve

```bash
ycode serve
```

The server prints its proxy URL and the composite MCP endpoint:
```
ycode MCP at       http://127.0.0.1:58080/mcp/  (ceiling: DangerFullAccess)
```

### 2. Pair with opencode

```bash
ycode pair --tool opencode --url http://127.0.0.1:58080
```

Paste the printed JSONC into `~/.opencode/opencode.jsonc` (or your
project's `.opencode/opencode.jsonc`). The shape is a single MCP entry:

```jsonc
{
  "mcp": {
    "ycode": {
      "type": "remote",
      "url": "http://127.0.0.1:58080/mcp/",
      "headers": { "Authorization": "Bearer <token>" },
      "timeout": 30000
    }
  }
}
```

opencode's MCP client (`packages/opencode/src/mcp/index.ts`) auto-detects
the remote server on next launch and fetches `tools/list`.

### 3. Restart opencode

The new tools appear with the `ycode_` prefix opencode applies to MCP
servers (`packages/opencode/src/mcp/index.ts:695` —
`<sanitized-server>_<sanitized-tool>`). Sample names visible to the LLM:

| Family | Tools |
|---|---|
| shell | `ycode_agent_shell` |
| treesitter | `ycode_list_symbols`, `ycode_search_symbols_by_pattern`, `ycode_find_symbol_references` |
| skills | `ycode_list_skills`, `ycode_get_skill` |
| gitea | `ycode_list_repos`, `ycode_create_pull_request`, `ycode_list_issues`, … |
| loom | `ycode_loom_lease`, `ycode_loom_push`, `ycode_loom_merge`, `ycode_loom_status`, `ycode_loom_release` |
| pulse | `ycode_query_traces`, `ycode_query_logs`, `ycode_query_metrics`, … |

## Calling shell from opencode

`ycode_agent_shell` requires a `cwd` argument when called over HTTP
(stdio callers can omit it — they inherit the agent process's cwd):

```json
{
  "name": "ycode_agent_shell",
  "arguments": {
    "command": "ls -la",
    "cwd": "/Users/you/projects/your-project"
  }
}
```

The server validates `cwd`: must be absolute, must exist, must be a
directory. Relative or missing paths return an envelope with a non-zero
exit code and a structured error in `stderr` — no command runs. Each
call gets its own bash session at the requested cwd; the shared runtime
is never mutated, so concurrent opencode sessions on different projects
don't collide.

The LLM should pass its own cwd (opencode exposes the project root
via env vars its built-in `shell` tool already consumes). The same
project path used by opencode's built-in shell is the right value here.

### Why the agent picks ycode_agent_shell over opencode's built-in shell

opencode ships its own `shell` tool (`packages/opencode/src/tool/shell.ts`).
Both coexist; the LLM picks based on tool descriptions. `ycode_agent_shell`
adds value when:

- The LLM wants in-process `yc <verb>` builtins (`yc symbols`,
  `yc search-symbols`, `yc refs`, `yc repomap`, `yc graph`, `yc git`,
  `yc remember`, `yc recall`, `yc browser`, `yc sandbox`).
- Pre/post-exec hints from ycode's agent-mode catalog are useful
  (e.g. suggesting `yc search-symbols` when the agent ran `grep -r`).
- Telemetry should flow through ycode's pulse stack (the call lands in
  ycode's OTel collector either way).

For plain bash the built-in is fine.

## Per-agent expectations

opencode's `Agent` system (`packages/opencode/src/agent/agent.ts`) gates
tool calls per-agent. The defaults are:

- **plan agent** — read-only. Denies `bash`, denies edits. opencode
  treats `ycode_agent_shell` as a write-class tool, so it is also denied
  here. Read tools — `ycode_list_symbols`, `ycode_loom_status`,
  `ycode_query_traces` — work.
- **build agent** — full access. All `ycode_*` tools available.
- **scout agent** — read-only with relaxed rules; bash denied.

Permission denials happen on opencode's side; the request never reaches
ycode. ycode's own ceiling (`--mcp-permission`, default
`danger-full-access`) is a server-side defense in depth — set it to
`workspace-write` or `read-only` if you want a stricter cap that opencode
can't override.

## Loom worked example: opencode + 3 sub-agents

opencode ships its own `Worktree` service (`packages/opencode/src/worktree/`)
for sub-agent isolation. Loom is a different primitive: it gives each
sub-agent an isolated **Gitea-backed** clone+branch+author identity that
converges through ycode's merger/CI gate. They are not redundant — pick
loom when you want PR-driven convergence with auto-merge on green CI;
pick opencode worktrees for purely local-staging branches.

```text
1. Parent agent calls ycode_loom_lease per sub-agent
   → returns {loom_id, path, branch, clone_url, author_*}
2. Parent spawns sub-agents, each working in cwd=lease.path
3. As each finishes:
     ycode_loom_push  → commit + push lease branch
     ycode_loom_merge → open PR against main
4. Parent polls ycode_loom_status; merger ticks every 10s,
   runs CI per PR, merges on green via Gitea's API
5. Parent calls ycode_loom_release per lease
```

See [loom.md](./loom.md) for the standalone walkthrough.

## Telemetry: opencode → pulse

opencode honors standard OTLP env vars. Point it at ycode's collector:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
export OTEL_SERVICE_NAME=opencode
```

opencode's spans/metrics/logs land in pulse alongside ycode's own and
any other `ycode pair --tool` client. Already covered in
[integration-foreign-agents.md](./integration-foreign-agents.md).

## Common gotchas

- **bash denied in plan mode.** opencode's `plan` agent denies write-class
  tools, including `ycode_agent_shell`. Switch to the `build` agent.
- **Token rotation invalidates the snippet.** `rm
  ~/.agents/ycode/server.token && ycode pair --tool opencode` to re-pair.
- **`cwd` is required over HTTP.** The LLM must pass an absolute path.
  Relative or missing paths surface as a structured stderr error in the
  tool's response — opencode's LLM sees the error and can retry with the
  right path.
- **Server ceiling vs opencode permission.** ycode enforces
  `min(server-ceiling, client-hint)`. If the server runs with
  `--mcp-permission read-only`, build-agent calls to write tools fail at
  ycode regardless of opencode's allow-list.
- **Tool name prefix.** opencode prefixes with the MCP server name. The
  ycode tool `loom_lease` becomes `ycode_loom_lease` to the LLM. Adjust
  any custom prompts/skills that reference unprefixed names.

## Verification (smoke test)

After steps 1-3, ask opencode (in build mode):

> "Run `pwd` here — what directory are we in?"

Expected: opencode's LLM calls `ycode_agent_shell` with `command=pwd` and
`cwd=<your project root>`. The envelope's `stdout` matches your project
path. If opencode's built-in `shell` was selected instead, the LLM should
still produce the right answer — both tools work; this just confirms the
ycode path is reachable.

Then:

> "List the loom lease status."

Expected: opencode calls `ycode_loom_status` (read-only, allowed in plan
mode too). Returns `[]` if no leases are active.

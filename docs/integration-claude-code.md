# Claude Code → ycode integration

[Claude Code](https://docs.claude.com/en/docs/claude-code) speaks MCP
natively. Pointing it at a running `ycode serve` exposes ycode's bash,
treesitter, skills, gitea, loom, and pulse families as `mcp__ycode__*`
tools that Claude's LLM can call directly. No plugins, no subprocess.

This is the canonical recipe for the `ycode pair --tool claude-code`
flow. For the cross-tool guide see
[integration-foreign-agents.md](./integration-foreign-agents.md). For
opencode see [integration-opencode.md](./integration-opencode.md).

## Steps

### 1. Start ycode serve

```bash
ycode serve
```

The server prints its proxy URL and the composite MCP endpoint:
```
ycode MCP at       http://127.0.0.1:31415/mcp/  (ceiling: DangerFullAccess)
```

### 2. Pair with Claude Code

```bash
ycode pair --tool claude-code --url http://127.0.0.1:31415
```

The printed JSON is a single MCP entry:

```json
{
  "mcpServers": {
    "ycode": {
      "type": "http",
      "url": "http://127.0.0.1:31415/mcp/",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

Two valid destinations (Claude Code reads both):

- **`.mcp.json` at the project root** — project-scoped, the recommended
  spot when you want this pairing only inside one tree. Claude Code
  reads it from `getCwd()` (`reference/claude-code/services/mcp/config.ts:89`).
- **`~/.claude/settings.json`, under the `mcpServers` key** —
  user-global, applies to every Claude Code session. Same shape, just
  nested under `mcpServers` in the wider settings object.

`~/.mcp.json` (without the `.claude/` prefix) is **not** a Claude Code
path — don't use it.

### 3. Restart Claude Code

The new tools appear with the `mcp__ycode__` prefix Claude Code applies
to MCP servers (`reference/claude-code/services/mcp/mcpStringUtils.ts:39-51`).
Sample names visible to the LLM:

| Family | Tools |
|---|---|
| shell | `mcp__ycode__agent_shell` |
| treesitter | `mcp__ycode__list_symbols`, `mcp__ycode__search_symbols_by_pattern`, `mcp__ycode__find_symbol_references` |
| skills | `mcp__ycode__list_skills`, `mcp__ycode__get_skill` |
| gitea | `mcp__ycode__list_repos`, `mcp__ycode__create_pull_request`, `mcp__ycode__list_issues`, … |
| loom | `mcp__ycode__loom_lease`, `mcp__ycode__loom_push`, `mcp__ycode__loom_merge`, `mcp__ycode__loom_status`, `mcp__ycode__loom_release` |
| pulse | `mcp__ycode__query_traces`, `mcp__ycode__query_logs`, `mcp__ycode__query_metrics`, … |

The `mcp__<server>__` prefix means the ycode tools never collide with
Claude Code built-ins (e.g. `Bash`, `Read`, `Grep`).

## Calling shell from Claude Code

`mcp__ycode__agent_shell` requires a `cwd` argument when called over
HTTP (an absolute path; relative or missing paths return a structured
error in stderr without running the command):

```json
{
  "command": "ls -la",
  "cwd": "/Users/you/projects/your-project"
}
```

The LLM should pass its own project root (Claude Code already exposes
this via env). Each call gets its own bash session at the requested
cwd; the shared runtime is never mutated, so concurrent Claude sessions
on different projects don't collide.

### Why pick mcp__ycode__agent_shell over Claude Code's built-in Bash

Claude Code ships its own `Bash` tool. Both coexist; the LLM picks
based on tool descriptions. Use `mcp__ycode__agent_shell` when:

- The LLM wants in-process `yc <verb>` builtins (`yc symbols`,
  `yc search-symbols`, `yc refs`, `yc repomap`, `yc graph`, `yc git`,
  `yc remember`, `yc recall`, `yc sandbox`).
- Pre/post-exec hints from ycode's agent-mode catalog are useful
  (e.g. suggesting `yc search-symbols` when the agent ran `grep -r`).
- Telemetry should flow through ycode's pulse stack.

For plain bash the built-in is fine.

## Permission modes

Claude Code has 6 permission modes
(`reference/claude-code/permissions/...`):

| Mode | What ycode tools see |
|---|---|
| `bypassPermissions` | All `mcp__ycode__*` tools allowed without prompts. |
| `acceptEdits` | File-touching tools allowed; `mcp__ycode__agent_shell` and `mcp__ycode__loom_*` writes prompt. |
| `plan` | Read-only — only `mcp__ycode__list_*`, `mcp__ycode__loom_status`, `mcp__ycode__query_*` allowed; bash and writes blocked. |
| `default` | Safe tools (`Read`, `Glob`, `Grep`, `LS`, `ToolSearch`) allowed; everything else, including all `mcp__ycode__*` tools, prompts. |

Permission decisions happen on the Claude Code side; the request never
reaches ycode if the user denies. ycode's own `--mcp-permission`
ceiling (default `danger-full-access`) is server-side defense in depth
— set it to `workspace-write` or `read-only` for a stricter cap that
Claude Code can't override.

## Loom worked example

Claude Code dispatches sub-agents via its `Agent` tool. To give each
sub-agent an isolated git workspace (Gitea-backed clone + branch +
author identity that converges through ycode's merger/CI gate), use
loom:

```text
1. Parent calls mcp__ycode__loom_lease per sub-agent
   → returns {loom_id, path, branch, clone_url, author_*}
2. Parent spawns sub-agents via the Agent tool, each working in
   cwd=lease.path
3. As each finishes:
     mcp__ycode__loom_push  → commit + push lease branch
     mcp__ycode__loom_merge → open PR against main
4. Parent polls mcp__ycode__loom_status; merger ticks every 10s,
   runs CI per PR, merges on green via Gitea's API
5. Parent calls mcp__ycode__loom_release per lease
```

See [loom.md](./loom.md) for the standalone walkthrough.

## Telemetry: Claude Code → pulse

Claude Code honors standard OTLP env vars. Point it at ycode's
collector:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
export OTEL_SERVICE_NAME=claude-code
```

Claude Code's spans/metrics/logs land in pulse alongside ycode's own
and any other paired client.

## Common gotchas

- **`~/.mcp.json` is not a Claude Code path.** Use project `.mcp.json`
  or `~/.claude/settings.json`.
- **Tool prefix differs from opencode.** Claude Code: `mcp__ycode__loom_lease`.
  opencode: `ycode_loom_lease`. Same ycode server feeds both — the
  prefix is the client's choice, not ycode's.
- **Token rotation invalidates the snippet.** `rm
  ~/.agents/ycode/server.token && ycode pair --tool claude-code` to
  re-pair.
- **`cwd` is required over HTTP.** The LLM must pass an absolute path
  to `mcp__ycode__agent_shell`. Relative or missing paths return a
  structured stderr error.
- **Server ceiling vs Claude Code permission.** ycode enforces
  `min(server-ceiling, client-policy)`. If the server runs with
  `--mcp-permission read-only`, write tools fail at ycode regardless
  of Claude Code's mode.

## Better integration: future paths

The MCP path above is production-stable today. Two surfaces in Claude
Code could yield tighter integration if the marginal value is worth
the build:

- **Plugin manifest** (`reference/claude-code/plugins/`,
  `types/plugin.ts`). A plugin at `~/.claude/plugins/ycode/plugin.json`
  could pre-bundle the MCP entry so users get one-click install
  through Claude Code's marketplace, plus optional bundled hooks and
  skills. Highest leverage; not yet shipped on the ycode side.
- **`PostToolUse` hook** (`reference/claude-code/schemas/hooks.ts`).
  Lets ycode observe every Claude tool invocation post-execution and
  feed telemetry/scoring back into ycode's memex. Observation-only —
  hooks fire after the tool runs, can't change tool selection — but a
  good signal channel for "what does the LLM actually do here".

Neither is required. The MCP path is sufficient for everything bash +
gitea + loom needs today.

## Verification (smoke test)

After steps 1-3, in a Claude Code session:

> "Run `pwd` here using the ycode MCP server."

Expected: Claude calls `mcp__ycode__agent_shell` with `command=pwd`
and `cwd=<your project root>`. The envelope's `stdout` matches your
project path.

Then:

> "List the loom lease status."

Expected: Claude calls `mcp__ycode__loom_status` (read-only, allowed
in plan mode too). Returns `[]` if no leases are active.

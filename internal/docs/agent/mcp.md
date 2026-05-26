---
topic: mcp
summary: connect to ycode's MCP composite endpoint or stdio server
when: you've never used ycode before, or an MCP tool you expected isn't in your tool list
audience: agent
max_lines: 120
---

ycode ships two MCP servers. You almost always want one of them already
configured in your agent before you reach for shelling out to `yc` or
`ycode` directly. Probe before assuming neither is present.

## When to use this

- You're starting a task in a repo that might be ycode-aware and you
  want to know what MCP tools exist before falling back to shell.
- Your tool list has no `mcp__ycode__*` or `mcp__ycode-stdio__*` entries
  but the user mentions ycode capabilities (loom, pulse, sandbox, etc.).
- An MCP call returns "connection refused" or "unauthorized".

## Live discovery (do this first)

Read `~/.agents/ycode/manifest.json`. The relevant keys:

- `mcp.http.ycode` — HTTP composite endpoint (requires `ycode serve` to
  be running on this host). Absent or unreachable → HTTP MCP is offline.
- `mcp.stdio.command` / `mcp.stdio.args` — the binary to spawn for the
  stdio server. Always available if the `ycode` binary is on PATH.
- `auth.tokenFile` — bearer token path for the HTTP endpoint.

If the manifest file is missing entirely, ycode is not installed or has
never run on this host; nothing to use.

## The two servers

**stdio** (`ycode mcp serve`): zero-setup, read-only by default.
Exposes treesitter symbol search and skills today; more families land
as they ship. Use from any MCP-aware agent (Claude Code, Codex,
Cursor) by configuring its mcpServers block to spawn the command.

**HTTP** (`http://127.0.0.1:<port>/mcp/`): richer surface — adds loom
(workspaces), pulse (observability), gitea (git server), sandbox, etc.
Requires `ycode serve` running. Auth is a single bearer token; pair a
new client with `ycode pair --tool <name>`.

## Exact calls

- Inspect what's live: `cat ~/.agents/ycode/manifest.json` (or read
  the file directly through your fs tool).
- Pair a client: `ycode pair --tool claude-code` (or `opencode`,
  `codex`, `gemini-cli`, `ycode-tui`, or `generic`).
- Start the HTTP server if absent: `ycode serve` (background; advertises
  itself via the manifest once ready).
- List tools the HTTP endpoint exposes:
  `curl -H "Authorization: Bearer $(cat ~/.agents/ycode/server.token)" \
    -X POST http://127.0.0.1:<port>/mcp/ \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`
- Per-call permission tier (works on both transports): pass
  `"_meta": {"permission": "ReadOnly"}` in your MCP `tools/call` body.

## Failure modes

| Symptom | Fix |
|---|---|
| `connection refused` on HTTP `/mcp/` | `ycode serve` not running; start it. |
| `401 Unauthorized` | Token mismatch; re-run `ycode pair`. |
| `tools/list` returns `[]` | Composite handler still booting; wait or check `ycode help` for the serve subcommands. |
| Stdio tools appear but HTTP ones don't | HTTP server offline — fall back to the stdio family. |

---
topic: mcp
summary: connect to ycode's MCP composite HTTP endpoint
when: you've never used ycode before, or an MCP tool you expected isn't in your tool list
audience: agent
max_lines: 120
---

ycode ships an MCP server mounted by `ycode serve` at its HTTP
`/mcp/` endpoint. Probe before assuming it isn't present.

## When to use this

- You're starting a task in a repo that might be ycode-aware and you
  want to know what MCP tools exist before falling back to shell.
- Your tool list has no `mcp__ycode__*` entries
  but the user mentions ycode capabilities (loom, pulse, sandbox, etc.).
- An MCP call returns "connection refused" or "unauthorized".

## Live discovery (do this first)

Read `~/.agents/ycode/manifest.json`. The relevant keys:

- `mcp.http.ycode` — HTTP composite endpoint (requires `ycode serve` to
  be running on this host). Absent or unreachable → HTTP MCP is offline.
- `auth.tokenFile` — bearer token path for the HTTP endpoint.

If the manifest file is missing entirely, `ycode serve` has never run on
this host.

## The server

**HTTP** (`http://127.0.0.1:<port>/mcp/`): the composite MCP endpoint.
Exposes treesitter (`list_symbols`,
`search_symbols_by_pattern`, `get_supported_languages`), shell
(`agent_shell`), skills, docs (`list_docs` / `get_doc` / `list_catalog`),
cobra runner (`list_ycode_commands` / `run_ycode_command{,_workspace}`),
document extractor (`extract_document`), repomap (`build_repomap`,
`repomap_for_files`), code graph (`graph_*`), delegated sandbox
(`sandbox_exec`), GitHub (`github_*`), **loom** (`loom_lease`,
`loom_push`, `loom_merge`, `loom_status`, `loom_release`), and
when the memex store is reachable — memex
(`memex_save`, `memex_recall`, `memex_list`, `memex_forget`,
`memex_index`, `search_memex`, `list_memory_types`). Requires
`ycode serve` running. Auth is a single bearer token; pair a new client
with `ycode pair --tool <name>`.

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
- Per-call permission tier: pass
  `"_meta": {"permission": "ReadOnly"}` in your MCP `tools/call` body.

## Failure modes

| Symptom | Fix |
|---|---|
| `connection refused` on HTTP `/mcp/` | `ycode serve` not running; start it. |
| `401 Unauthorized` | Token mismatch; re-run `ycode pair`. |
| `tools/list` returns `[]` | Composite handler still booting; wait or check `ycode help` for the serve subcommands. |

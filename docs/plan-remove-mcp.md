# Plan: remove MCP from ycode — be driven, not exposed

*Scope doc, 2026-07-14. Direction set by the user; execution phased so each step builds green.*

## Why

ycode's value is being a **lightweight, steerable executor** — driven by bashy into roles
(steward for the host, secretary for `meet`, conductor for the SDLC), reporting turn
boundaries as *facts* over the `--events` channel. That is the first-party-harness thesis
(`docs/first-party-harness.md`).

MCP is the opposite model: a plugin surface that **exposes** ycode's capabilities to foreign
agents "without shell exec" (its own `mcp serve` help says so). But in this stack **bash is
the exec surface** — an agent with bashy in PATH runs `bashy ast symbols`, it does not want a
JSON-RPC server, a schema to maintain, and a stdio round-trip for the same bytes. The
code-intel MCP exposes (`symbols`/`refs`/`graph`/`repomap`) is already duplicated by bashy's
`ast`/`graph` — the same redundancy that retired `yc`.

So: **MCP-as-local-exposure is obsoleted by bashy-as-shell.** Steerability (events) replaces
exposure (MCP). Remove it.

Nuance preserved: MCP is *not* dead for **remote/hosted** capabilities (Gmail, hosted DBs, the
pooled-LLM gateway). But reaching those is bashy's job (the provisioned runtime / gateway), not
the coding harness's. ycode does not need to be an MCP client either.

## Blast radius (measured)

MCP is woven through ycode, not bolted on:

- **Standalone command** — `cmd/ycode/mcp.go` + `cmd/ycode/cmdmcp.go` (615 lines).
- **serve endpoint** — `/mcp/` in `cmd/ycode/serve.go`: `buildServeMCPHandlers` wires 7+
  capability adapters (treesitter, skills, shell, repomap, docs, memexmcp, widget).
- **selfinit interface** — `WriteMCP` is a method on the `SelfInit` interface
  (`internal/selfinit/types.go`), implemented by `claude.go` + `opencode.go`, called from
  `selfinit.go`. Every `ycode init` writes ycode into `~/.claude.json` /
  `opencode.json` under `mcpServers`. **This is the auto-registration that makes ycode an MCP
  tool in other agents — the clearest thing to stop.**
- **Per-capability adapters** — `mcpserver.go` in ~10 packages (treesitter, repomap, codegraph,
  memexmcp, github, skills, docs, shell, extractmcp, widget). Each exposes a real capability
  via MCP; the capability stays, the adapter goes.
- **The machinery** — `internal/runtime/mcp/` (16 files: server *and* client — bridge,
  composite, crosstransport, stdio, sse, lifecycle, client, config, …).

~17 files import `runtime/mcp`; ~30 files touched in total.

## Phases (each ends green + committed)

**P1 — stop exposing ycode as an MCP tool (the outward face).**
  - Delete the standalone `ycode mcp` command (`mcp.go`, `cmdmcp.go`, its registration).
  - Remove `WriteMCP` from the `SelfInit` interface + `claude.go`/`opencode.go` impls + the
    `selfinit.go` caller (+ tests). `ycode init` stops polluting `~/.claude.json` /
    `opencode.json`. **Highest-value, most self-contained — do first.**
  - Leaves `runtime/mcp` + adapters compiling (serve still uses them) — intermediate state is
    valid.

**P2 — remove the serve `/mcp/` endpoint.**
  - Drop `buildServeMCPHandlers`, the `/mcp/` mount, `newMCPHTTPHandler`, and the
    `NewMCPHandler()` calls from `serve.go`. serve keeps API/WebSocket/NATS/manifest/pprof.
  - After this nothing constructs the per-capability adapters.

**P3 — delete the per-capability `mcpserver.go` adapters.**
  - Remove `mcpserver.go` from treesitter/repomap/codegraph/memexmcp/github/skills/docs/shell/
    extractmcp/widget. The capabilities (parsing, repomap, codegraph, …) stay; only the MCP
    projection goes. `internal/extractmcp` and `internal/docs/mcpserver.go` likely delete whole.

**P4 — delete `internal/runtime/mcp/` (server + client).**
  - Once nothing imports it. Decision recorded here: **remove the client too** — consuming
    remote MCP is a gateway concern (bashy), not the harness's, and the vision is shell-based
    + steerable, not plugin-based.
  - Prune `mcp`-related fields from `config.go` / settings schema.

**P5 — sweep.**
  - `internal/tools/mcp_tools.go`, `sandbox_mcp.go`, `handlers_inventory.go`, `tools_cmd.go`
    references; `otel/middleware.go` MCP span attrs; docs + `ycode docs`/`--help` text; the
    treesitter MCP server that CLAUDE.md advertises as `ycode-stdio` (update the umbrella note —
    that capability now lives as `bashy ast`).

## Acceptance

- `go build ./...` + `go test ./...` green after each phase.
- `ycode init` writes **no** `mcpServers` entry (P1).
- `ycode --help` shows no `mcp` command; `ycode serve` advertises no `/mcp/` (P1/P2).
- `grep -rn "runtime/mcp" --include=*.go` returns nothing (P4).
- Windows cross-build unaffected (note: ycode already has a *pre-existing* unix-only issue in
  `internal/runtime/bash/exechandler.go` — `Setpgid`/`syscall.Kill` — out of scope here).

## Not in scope

The steward/secretary/conductor **role TUI** vision this enables is separate downstream work:
ycode as a steerable front-end bashy drives into `steward` / `meet` / `conductor` roles over
the event channel. Removing MCP clears the deck for it; building it is its own plan.

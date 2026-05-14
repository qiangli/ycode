# Lighthouse Roadmap

The canonical plan, with full rationale and decision history, lives at `~/.claude/plans/ycode-is-an-agentic-delegated-lighthouse.md`. This file is the in-repo summary so the strategic frame survives across sessions and a fresh agent can resume cold.

The contributor-facing companion is [`docs/lighthouse.md`](./lighthouse.md) — that's HOW to add a capability. This doc is WHAT we decided and WHERE we are.

## The strategic frame (from session 2026-05-07)

**The wedge, restated through this work.** ycode bundles capabilities most coding agents lack — polyglot AST search, podman sandbox, embedded Ollama for cheap subtasks, code-knowledge graph, embedded Gitea for isolated workspaces, OTEL collector, skills registry. The lighthouse pattern makes those reachable from any third-party agent (Claude Code, Codex, Cursor, Continue, older ycode builds) over MCP, without plugins or shell exec.

**Two operating modes.**
- **Mode 1** — agent works inside the ycode source tree. Committed `.mcp.json` at repo root + `bin/ycode mcp serve` is the in-tree beam.
- **Mode 2** — agent works in any other codebase. `~/.agents/ycode/manifest.json` is user-home global, so it's already discoverable. A `ycode init-lighthouse <tool>` subcommand (Phase-2 deliverable) writes per-tool global MCP config.

**The bidirectional flywheel.** A foreign agent in this tree can either *consume what exists* or *create what's missing* — adding a new capability is one new `mcpserver.go` per family. Same session that lands a capability can use it. Inbound and outbound flow over one surface.

**Mode 2 = ecosystem-wide self-improvement substrate.** Foreign agents using ycode in *other* codebases ship spans into ycode's collector, store memories in ycode's memex, accrue stats in ycode's skill registry, and emit capability-gap signal every time they shell out for something ycode could expose. ycode's autonomous-loop EVALUATE phase has multi-agent signal, not just ycode-on-ycode signal. Caveats: opt-in privacy + provenance-tagged signal weighting are non-negotiable deliverables.

**The matrix.** Once foreign agents themselves expose MCP servers, ycode can route between them — Cursor's index reachable from Claude Code, Claude Code's memory reachable from Cursor, all over MCP, with ycode as the transport. ycode stops competing with the agents and becomes the **mesh fabric**. Every connected agent is simultaneously producer and consumer; the matrix gets richer with each agent that joins. Mechanism is the `mcp_proxy_call` tool from Family E (already in plan); the work is policy and ergonomics, not protocol.

**Federation discipline (non-negotiable).** ycode is **the hub of *your* matrix, never the matrix's hub** — singular possessive, not definite article. Every user runs ycode locally; ycode never phones home; two ycode instances can optionally federate but federation is opt-in and peer-to-peer. The mental model is email/git/ActivityPub. Centralized routing would directly contradict the local-first wedge and would make ycode a chokepoint competitors route around. The systems that won at this scale all chose the federated path.

**Positioning anchor.** *"OpenWebUI is the local hub for humans talking to LLMs; ycode is the local hub for agents talking to each other and to your codebase."* Same architectural shape, different surface (API not UI), different "users" (programs not humans), bigger network-effect phase space (agents × capabilities-per-agent). The shortcut is legibility — a recognizable shape, not a novel one.

## What shipped — Phase 0 (commit `94fee14`)

Infrastructure only; no capability surface yet (`tools/list` returns `[]`). The point of Phase 0 is unblocking everything that follows.

- `cmd/ycode/mcp.go` — new `ycode mcp serve` cobra subcommand, stdio MCP server, default `StaticGate{Ceiling: ModeReadOnly}`.
- `internal/runtime/mcp/composite.go` — `CompositeHandler` aggregates multiple `ServerHandler`s, routes `tools/call` by name, delegates `RequiredMode` so per-family permission posture flows to the gate.
- `internal/runtime/mcp/permission.go` — `PermissionMode`, `PermissionAware` (optional interface), `PermissionGate`, `StaticGate`, `GatedHandler`.
- `cmd/ycode/manifest.go` — writes `~/.agents/ycode/manifest.json` at `ycode serve` startup with every live endpoint.
- `.mcp.json` at repo root — committed lighthouse beam for Mode 1.
- `internal/eval/contract/mcpserve_validation_test.go` + `internal/runtime/mcp/permission_test.go` — contract tests asserting `tools/list` returns `[]` not `null`, gate denies above-ceiling, composite routes correctly, unknown tools error.
- `docs/lighthouse.md` — contributor guide.
- `AGENTS.md` — added "Foreign agents" section.

Judgment call worth remembering: the entry-point subcommand is **intentionally not** behind the `experimental` build tag, even though `docs/strategy.md` says new features should be. Reason: tagging the entry point would break the committed `.mcp.json` for default-build binaries and defeat Phase 0's purpose. Tier discipline lives at the *capability* level (Phase 1+), not at the routing-and-framing layer.

## What's queued — Phase 1+

The plan groups families in priority order. Each new capability is a single `mcpserver.go` mirroring `internal/observability/mcpserver.go` (`TelemetryHandler`).

**Phase 1 — Family A (code understanding) + Family C (workspaces).** Highest-leverage pair.
- A.1 AST/treesitter — `internal/runtime/treesitter/mcpserver.go`. Recommended tools: `list_symbols`, `search_symbols_by_pattern`, `search_ast_query`, `find_symbol_references`, `get_supported_languages`. All `ReadOnly`.
- A.2 Repo-map — `internal/runtime/repomap/mcpserver.go`. **HTTP-shipped.** Tools: `build_repomap` and `repomap_for_files`, both `ReadOnly`. Stateless. Registered in stdio (`cmd/ycode/mcp.go`) and in HTTP composite (`cmd/ycode/serve.go`). Contract: `internal/eval/contract/repomap_mcp_validation_test.go`.
- A.3 Memex memory — `internal/runtime/memexmcp/mcpserver.go` (kept in internal/ to preserve `pkg/memex/memory` as pure public API). **HTTP-shipped.** Tools: `memex_recall`, `memex_save` (WorkspaceWrite), `memex_list`, `memex_forget` (WorkspaceWrite), `memex_index`, `search_memex` (RRF source filter), `list_memory_types`. `PermissionAware`. Privileged scopes (`global`/`user`/`team`) are rejected at the MCP boundary regardless of gate — operator must write those through the ycode CLI/TUI directly. Contract: `internal/eval/contract/memex_mcp_validation_test.go`.
- A.4 Memex graph — `pkg/memex/graph/mcpserver.go`. `query_graph_dql`, `graph_neighborhood`, `graph_path`. `ReadOnly`. Thin wrap over the existing `:58080/graph/` endpoint.
- C **Loom (workspace substrate) — shipped.** Foreign agentic tools (Claude Code, OpenCode, Codex, Gemini CLI) hand each of their sub-agents an isolated clone+branch+author identity via a 5-verb MCP surface (`loom_lease`, `loom_push`, `loom_merge`, `loom_status`, `loom_release`). Implementation: public Go API `pkg/loom`, gitea-backed adapter + JSON-RPC handler `internal/gitserver/loom`, fanned out via the composite `/mcp/` endpoint under `ycode serve`. Convergence via the existing merger/CI gate. Contract: see [`docs/loom.md`](./loom.md).

**Phase 2 — Family D (inference) + Family G (procedural reuse).** Ollama proxy → cheap-LLM subtasks. Skills/swarms → ycode's procedural knowledge as MCP tools. **D HTTP-shipped.** `internal/inference/mcpserver.go` exposes `ollama_list_models`, `ollama_chat`, `ollama_embed` (all `ReadOnly`). Default 5-minute per-request timeout, overridable via `YCODE_OLLAMA_MCP_TIMEOUT`. HTTP composite registration threads `stack.ollama.BaseURL()` when the managed runner is healthy, otherwise falls back to the env-then-default chain. Note: `ollama_chat` is read-only from a filesystem-effect perspective but consumes local compute — a Phase-4 `ModeOutboundNetwork` tier would tighten this. Contract: `internal/eval/contract/inference_mcp_validation_test.go`.

**Phase 3 — Family B (code execution).** bash, file ops, web, podman, astgrep. Routes through the permission middleware. Where the security model gets exercised.

**Phase 4 — Family E (external) + Family H (coordination) + the matrix.** GitHub MCP, MCP proxy bridge (the matrix substrate), NATS pub/sub, session-steering. Per-pair allowlists, namespacing, three-hop provenance land here.

**Mode 2 deliverables (shipped — `internal/selfinit`, late 2026-05).** Reframed mid-design from "explicit `ycode init-lighthouse <tool>` per-tool installer" into the stronger "ycode auto-establishes as a first-class citizen in any git repo on every entry-point invocation": the cobra root command's `PersistentPreRun` runs `selfinit.Run`, which (a) writes `<repo>/.ycode/AGENTS.md` + patches `<repo>/AGENTS.md`/`CLAUDE.md`, and (b) detects installed agentic tools (Claude Code + OpenCode at v1; Codex + Gemini queued) and refreshes their user-scope MCP config + memory file. Idempotent via state-hash marker; per-repo opt-out via `<repo>/.ycode/.no-init`; global opt-out via `YCODE_NO_SELF_INIT=1` or `--no-self-init`. The same code path is reused by the explicit `ycode init` CLI subcommand and the embedded `/init` slash command — no drift between manual and automatic flows. See [`docs/selfinit.md`](./selfinit.md).

## Cross-references

- Canonical plan with full decision history: `~/.claude/plans/ycode-is-an-agentic-delegated-lighthouse.md`
- Contributor guide for adding capabilities: [`docs/lighthouse.md`](./lighthouse.md)
- Strategy doc — wedge, feature tiers, graduation criteria: [`docs/strategy.md`](./strategy.md)
- Architecture: [`docs/architecture.md`](./architecture.md)
- Foreign-agents pointer in [`AGENTS.md`](../AGENTS.md)

## Resuming work

Quickest path: open this directory in any coding agent, point it at this file plus `~/.claude/plans/ycode-is-an-agentic-delegated-lighthouse.md`, and say which task to pick up. Phase 0 is `94fee14`. Phase 1 starts with Family A.1 (AST/treesitter handler) — the treesitter package's public API was already mapped: `NewParser`, `Parse`, `ExtractSymbols`, `Search`, `SearchText`, `Analyze`, `SupportedLanguages`. The recipe is one new `mcpserver.go` + register it in `cmd/ycode/mcp.go`'s composite + add a contract test mirroring `internal/eval/contract/mcpserve_validation_test.go`.

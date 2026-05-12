# The Lighthouse Pattern

ycode bundles capabilities most coding agents do not have: polyglot AST search, a podman sandbox, an embedded Ollama for cheap subtasks, a code-knowledge graph, an embedded Gitea for isolated workspaces, an OTEL collector, the skills registry. The lighthouse pattern makes those capabilities reachable from any third-party coding agent (Claude Code, Codex, Cursor, Continue, an older ycode build) **without plugins or shell exec**, in two operating modes:

- **Mode 1 — agent works inside the ycode source tree.** The committed `.mcp.json` at the repo root points at `ycode mcp serve`. Any agent opened in this directory picks ycode up automatically.
- **Mode 2 — agent works in any other codebase.** Run `ycode init-lighthouse <tool>` once (Phase-2 deliverable; not yet implemented) and the same MCP surface is available from every project. The `~/.agents/ycode/manifest.json` written by `ycode serve` is already user-home global, so the discovery layer is ready.

Both modes share the same protocol layer (`ycode mcp serve`, stdio JSON-RPC with Content-Length framing) and the same capability layer (one MCP `ServerHandler` per family). The plan is in `docs/strategy.md` under "Lighthouse" (forthcoming) and the long-form rationale in the planning doc that produced this implementation.

## Adding a capability

Adding a new ycode capability to the MCP surface is a single file in five steps. The cost has to stay small; that is what keeps the outbound half of the flywheel cheap.

1. **Pick the underlying capability.** It already exists in `internal/...` or `pkg/...` — you are wrapping, not reimplementing.

2. **Implement `mcp.ServerHandler` in a new `mcpserver.go` next to the capability.** The canonical template is `internal/observability/mcpserver.go` (`TelemetryHandler`). Required methods:
   - `ListTools() []mcp.Tool` — static tool declarations with JSON-Schema input.
   - `ListResources() []mcp.Resource` — return `nil` if you have none.
   - `HandleToolCall(ctx, name, input) (string, error)` — dispatch by name; return a string that will be wrapped in MCP's `{"content":[{"type":"text","text":...}]}` envelope by the protocol layer.
   - `ReadResource(ctx, uri) (string, error)` — return an error if you have no resources.

3. **Implement `mcp.PermissionAware` if any tool is write-capable.** Map each tool name to its required mode (`ModeReadOnly`, `ModeWorkspaceWrite`, `ModeDangerFullAccess`). Handlers that do not implement this interface are treated as ReadOnly — fine for read-only families, dangerous if you add a write tool without thinking.

4. **Register the handler.** Two places, depending on transport:
   - For `ycode mcp serve` (stdio): add an instance to the `mcp.NewCompositeHandler(...)` call in `cmd/ycode/mcp.go`.
   - For `ycode serve` (HTTP): append the handler to `compositeMCP` in `cmd/ycode/serve.go` (around the always-on `treesitter`/`skills` block). The composite at `/mcp/` is the single HTTP MCP entrypoint; do not mount per-family routes.

5. **Add a contract test in `internal/eval/contract/`** mirroring `mcpserve_validation_test.go`. The test must boot the same handler chain `ycode mcp serve` builds and assert at least one `tools/call` round-trip per family. This is the graduation gate per `docs/strategy.md`.

That is the whole loop. Stable interface, copy-pasteable, no orchestration to learn.

## Why the entry point is not gated by the `experimental` build tag

`docs/strategy.md` requires new features to land behind the `experimental` build tag and graduate after meeting criteria. The `ycode mcp serve` cobra subcommand intentionally does **not** carry the tag, because:

- The subcommand is inert without registered handlers — it speaks the protocol and returns an empty `tools/list`. That is a stable, tested surface (see `internal/eval/contract/mcpserve_validation_test.go`).
- If the entry point were tagged, the committed `.mcp.json` at the repo root would reference a binary that does not exist in default builds. The lighthouse beam would be broken for every default-build ycode binary, which defeats Phase 0's purpose.
- Each capability handler shipped in Phase 1+ carries its own `experimental` tag where strategy demands. Tier discipline lives at the *capability* level, where the risk actually is, not at the routing-and-framing layer.

Graduation criteria for individual handlers stay unchanged: integration test + documented failure modes + at least one cycle of dogfooding.

## Permission posture

The default permission gate for standalone `ycode mcp serve` is `StaticGate{Ceiling: ModeReadOnly}`. That means a fresh foreign agent gets only read-shaped tools by default — search, lookup, query. Anything that would write a file, run a shell command, or modify state is denied at the gate without prompting, because standalone stdio has no human-loop client to ask.

When the same handler chain is mounted inside `ycode serve` (Phase-1 deliverable, not yet implemented), the gate will be replaced with a prompting variant that routes through `RemotePermissionPrompter` — the same prompt path the in-process agent uses today. That gives Mode-2 foreign agents a real "approve-or-deny" UX at the host TUI/web client when they request escalated capabilities.

## The matrix (forthcoming, federated by design)

Once the MCP proxy bridge (Phase 4 of the strategy) is in place, ycode can route between agents — not just expose its own capabilities. A Claude Code session can call a tool a Cursor session is exposing, and vice versa, with ycode as the transport. Every connected agent becomes simultaneously a producer and a consumer; the matrix gets richer with each agent that joins.

Architectural rule, non-negotiable: **ycode is the hub of *your* matrix, not the matrix's hub.** Each user runs ycode locally. ycode never phones home. Two ycode instances can optionally federate (peer-to-peer) but federation is opt-in. The mental model is email/git/ActivityPub — federated, never centralized. Centralized routing would directly contradict the wedge and would make ycode a chokepoint competitors route around. The federation discipline is what keeps the wedge intact as the matrix grows.

## What's exposed today

Phase 0 ships infrastructure only. The `tools/list` over `ycode mcp serve` returns an empty array. Capability handlers (AST search, Gitea workspaces, Ollama proxy, memex, repo-map, podman, ...) land in Phase 1+ following the recipe above.

Already exposed via `ycode serve` HTTP MCP, all behind the single composite endpoint at `/mcp/`:

- treesitter — AST search across Go/Python/JS/TS/Rust/Java/C/Ruby (always on).
- skills — discover and resolve skills (always on).
- pulse — observability stack (~25 tools): trace/log/metric queries, diagnostics, dashboards, alerts.
- gitea — git server (~11 tools): repos, branches, PRs, issues.
- loom — workspace substrate: lease/push/merge/status/release.
- OTLP ingest at `127.0.0.1:4317` (gRPC) and `:4318` (HTTP) — any process can ship spans into ycode's collector.

The manifest at `~/.agents/ycode/manifest.json` advertises all of the above plus the stdio entry point.

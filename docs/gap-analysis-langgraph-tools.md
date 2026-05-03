# Gap Analysis: LangGraph — Built-in Tool System & Tool Use

**Tool:** LangGraph (Python framework for stateful multi-agent systems)
**Source:** `priorart/langgraph/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | LangGraph |
|------|-------|-----------|
| Semantic tool discovery | Deferred tools + Bleve search prevents token explosion for 100+ tools | Eager loading of all tools; no discovery mechanism |
| Permission model | Tiered modes (ReadOnly→WorkspaceWrite→DangerFullAccess) with per-tool policy rules | No permission model; all-or-nothing |
| Bash security | Destructive command detection, read-only allowlist, signal handling, sed interception, eval blocking | No native bash tool; relies on external wrappers |
| Container isolation | Podman-based sandboxing with resource limits for tool execution | No container support |
| File operation safety | Binary detection, sensitive file warnings, VFS path traversal prevention, encoding preservation | No file operations |
| Web tool security | SSRF protection, domain filtering, redirect validation, 1MB response limit | No web tools |
| Browser automation | Container-based Playwright/Puppeteer with domain allowlist | No browser tools |
| Category-aware scheduling | Interactive/Agent/Standard tool categories for concurrency control | Uniform parallelism only |
| LSP integration | Native code intelligence (definitions, references, hover, call hierarchy) | No LSP support |
| Background task management | Job registry for long-running commands with signal support | No background task concept |
| Tool output handling | Distillation, truncation, disk save, large output preview | Basic string/JSON return |

## Gaps Identified

| ID | Feature | LangGraph Implementation | ycode Status | Priority | Effort |
|----|---------|--------------------------|--------------|----------|--------|
| T1 | Tool call interception middleware | `wrap_tool_call` / `awrap_tool_call` enables retry, caching, request modification as composable wrappers around any tool invocation | ycode has per-tool hooks but no composable middleware chain for interception | Medium | Medium |
| T2 | Tool streaming with per-call writers | `StreamToolCallHandler` with context-var bound writers; emits tool-started/delta/finished/error events per tool call | ycode tools return complete results; no streaming partial output from within a tool | Medium | Medium |
| T3 | State injection into tools | `InjectedState`/`InjectedStore` annotations let tools access graph state without LLM seeing the parameters | ycode tools receive raw input JSON; no transparent state injection mechanism | Low | Low |

## Implementation Plan

### Phase 1: Tool Middleware Chain (T1)

**Files to modify:**
- `internal/tools/registry.go` — add `Middleware` type and middleware chain to `Registry`
- `internal/tools/middleware.go` — new file defining middleware interface and built-in middlewares

**Design:**
- `ToolMiddleware func(next ToolHandler) ToolHandler` — standard middleware signature
- Registry applies middleware chain on tool dispatch: logging → metrics → retry → actual handler
- Built-in middlewares: `WithRetry(maxAttempts, backoff)`, `WithTimeout(duration)`, `WithMetrics(collector)`
- Middleware is composable and per-tool configurable via `ToolSpec.Middleware []ToolMiddleware`

### Phase 2: Tool Streaming Events (T2)

**Files to modify:**
- `internal/tools/types.go` — add `ToolEventWriter` interface
- `internal/tools/registry.go` — inject writer into tool execution context

**Design:**
- `ToolEventWriter` interface: `WriteEvent(ToolEvent)` where ToolEvent has type (started/delta/finished/error) and payload
- Tools that support streaming can pull writer from context and emit deltas
- Non-streaming tools continue to work unchanged (writer is optional)
- Events feed into the stream subscriber system from A1

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| T3 | State injection into tools | ycode's tool handlers already have access to RuntimeContext; transparent injection is a framework pattern less relevant for a CLI agent |

## Verification

- `make build` passes with no errors
- Unit tests for middleware chain ordering and composition
- Unit tests for tool event writer
- Existing tool tests continue to pass

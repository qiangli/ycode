# Gap Analysis: Tool System & Tool Use — paperclip + openclaw + opencode

Combined analysis across three agentic tools vs ycode.

## Where ycode Is Stronger

| Area | ycode Advantage | Compared To |
|------|----------------|-------------|
| Native Git | 3-tier go-git (pure Go → filtered subprocess → full subprocess), 39.8K LOC | All three shell out to git; opencode wraps simple commands |
| Container Tools | Embedded Podman (no external binary), standardized pattern, bind mounts | Openclaw uses Docker subprocess; opencode/paperclip have no container tools |
| Fuzzy Edit | 4-level fallback (exact → line-trimmed → indent-normalized → block-anchor) | Opencode has diff-based fuzzy; openclaw/paperclip delegate to bash |
| Tool Semantic Search | Bleve-indexed deferred tool discovery with TTL-based activation | Openclaw has plugin caching; opencode has conditional availability; paperclip uses namespace lookup |
| Bash Safety | CommandIntent classification (8 categories, 20.5K LOC), AST-based shellparse | Openclaw has approval tiers + safe-binary list; opencode has tree-sitter parsing; paperclip has capability gating |
| Quality Monitor | Success/failure rate tracking per tool for reliability metrics | None of the three track per-tool quality |
| Browser Automation | Containerized browser-use with stateful multi-tab, LLM-optimized DOM | Openclaw uses MCP stdio; opencode has web fetch only; paperclip has no browser |
| SearXNG Search | Containerized meta-search (AGPL-safe HTTP-only boundary) | Openclaw has web-search providers; opencode uses Exa MCP; paperclip delegates to adapters |
| Device Protection | Blocks writes to /dev, /proc, /sys + binary file detection | Openclaw has workspace-only policy; opencode has none; paperclip relies on worker isolation |

## Gaps Identified

| ID | Feature | Source Tool | ycode Status | Priority | Effort |
|----|---------|-------------|-------------|----------|--------|
| T1 | Tool output streaming to disk for large outputs | opencode | Partial — ringbuffer truncates but doesn't stream to disk | High | Small |
| T2 | Reusable stall watchdog for tool execution | openclaw | Missing — timeouts exist but no generic watchdog | Medium | Small |
| T3 | Per-file edit locking (semaphore) | opencode | Missing — atomic writes but no concurrent edit protection | Medium | Small |
| T4 | Tree-sitter shell path extraction | opencode | Partial — shellparse exists but path extraction is basic | Low | Medium |
| T5 | Command arity dictionary for permissions | opencode | Missing — permissions are pattern-based without arity | Low | Small |
| T6 | Tool output preview during background exec | openclaw | Missing — no live preview for long-running tools | Low | Medium |

## Implementation Plan

### Phase 1: T1 — Tool Output Disk Spill (High)

**Rationale:** When tool output exceeds the ringbuffer limit, it's truncated. For autonomous operation, the full output should be preserved on disk for later retrieval. OpenCode's dual-buffer approach (memory + disk) with auto-cleanup is the model.

**Design:**
- New file: `internal/runtime/bash/output_spill.go`
- When output exceeds configurable threshold, spill to temp file
- Return truncated preview + file path in result
- Auto-cleanup spill files after configurable retention (7 days)
- Integration: post-execution in bash interpreter

### Phase 2: T2 — Stall Watchdog (Medium)

**Rationale:** A generic armable watchdog that detects when a tool execution stalls (no output for N seconds). Reusable across bash, container, and browser tools.

**Design:**
- New file: `internal/runtime/toolexec/stall_watchdog.go`
- Arm/Touch/Disarm API with configurable timeout
- Returns stall detection via channel
- Thread-safe, supports concurrent tool executions

### Phase 3: T3 — Per-File Edit Lock (Medium)

**Rationale:** When multiple subagents edit the same file concurrently, atomic writes prevent corruption but don't prevent lost updates. Per-file semaphores ensure serialized access.

**Design:**
- New file: `internal/runtime/fileops/filelock.go`
- Named mutex registry keyed by absolute path
- Lock/Unlock with context cancellation
- Auto-cleanup of stale locks

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| T4 | Tree-sitter shell path extraction | Existing shellparse sufficient; tree-sitter WASM adds complexity |
| T5 | Command arity dictionary | Current pattern matching adequate for CLI use |
| T6 | Tool output preview | Requires streaming architecture changes |

## Verification

- `go test -short -race ./internal/runtime/bash/...`
- `go test -short -race ./internal/runtime/toolexec/...`
- `go test -short -race ./internal/runtime/fileops/...`
- `make build` must pass

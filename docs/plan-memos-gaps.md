# Implementation Plan: Embedding Memos as ycode Memory Backend

## Phased Approach

Work is organized into 2 phases. Phase 1 delivers immediate value by embedding Memos as a proxy component. Phase 2 connects ycode's agent memory system to Memos' API.

**Scoping decisions:**
- Multi-user, OAuth/SSO features deferred — ycode is single-user
- gRPC client deferred — REST API sufficient for integration
- RSS, mobile UI deferred — not relevant for dev tool
- Memo relations, attachments deferred — nice-to-have, not core to memory use case

---

## Phase 1: Embed Memos as Proxy Component — Core Integration

### 1.1 Add go.mod replace directive (S) — Gap S1, S3
- Add `replace github.com/usememos/memos => ./external/memos` to go.mod
- Run `go mod tidy` to resolve dependency graph
- Resolve any version conflicts between ycode and memos dependencies

### 1.2 Create MemosComponent (S) — Gap S1, S2, F1
- Create `internal/observability/memos.go` implementing `Component` interface
- Import `github.com/usememos/memos/server`, `store`, `store/db`, `internal/profile`
- On `Start()`: create profile (SQLite, data dir under `~/.ycode/observability/memos/`), init DB driver, create store, run migrations, create server, start on ephemeral port
- On `Stop()`: call `server.Shutdown()`
- Expose `Port()` for reverse proxying

### 1.3 Register in stack manager (S) — Gap S2, F1
- Add `"memos": "/memos/"` to `componentPathMap` in `stack.go`
- Add `mgr.AddComponent(observability.NewMemosComponent(...))` in `buildStackManager()` in `serve.go`
- Memos will appear on the proxy landing page automatically

### 1.4 Build and test (S)
- Verify `go build ./...` succeeds
- Verify `ycode serve` starts with Memos accessible at `http://127.0.0.1:58080/memos/`
- Test basic Memos operations (create account, add memo, search)

---

## Phase 2: Agent Memory Integration (Future)

### 2.1 Memory API client (M) — Gap A1
- Create Go client for Memos REST API (`/api/v1/memos`)
- CRUD operations: create memo, list memos, search by tag/content, update, delete
- Auth: use instance admin token or local bypass

### 2.2 Connect agent memory to Memos (M) — Gap S4, A3
- Modify ycode's persistent memory layer to write to Memos via API
- Map memory types (episodic, semantic, persistent) to Memos tags
- Use Memos search for memory retrieval
- Keep vector store for semantic similarity; Memos for structured storage

### 2.3 Memory tools (S) — Gap A1
- Add agent tools: `memory_store`, `memory_search`, `memory_browse`
- These call Memos API under the hood
- Available to LLM during conversations

---

## Deferred

| # | Feature | Reason |
|---|---------|--------|
| A2 | gRPC client | REST API sufficient; gRPC adds complexity |
| S5 | Memo relations | Low priority; can add later if needed |
| S6 | Attachments | Not core to memory use case |
| U1, U2 | Multi-user/OAuth | ycode is single-user |
| A4 | RSS | Not relevant |
| F3 | Mobile UI | Not relevant for dev tool |

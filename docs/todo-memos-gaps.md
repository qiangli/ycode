# TODO: Memos Gap Implementation

Tracking checklist. See gap-analysis-memos.md for full analysis.

---

## Phase 1: Embed Memos as Proxy Component

> Goal: Run Memos as an embedded component accessible at `/memos/` on the proxy landing page.

- [x] **S1 — Add go.mod replace directive** (S)
  - [x] Add replace directive for `github.com/usememos/memos`
  - [x] Run `go mod tidy` and resolve conflicts

- [x] **S2 — Create MemosComponent** (S)
  - [x] Create `internal/observability/memos.go`
  - [x] Implement Component interface (Name, Start, Stop, Healthy, HTTPHandler)
  - [x] Start memos server on ephemeral port with SQLite storage
  - [x] Expose Port() for reverse proxying
  - [x] Create `external/memos/embed/embed.go` — public API wrapper for internal packages

- [x] **F1 — Register in stack manager** (S)
  - [x] Add `"memos"` to `componentPathMap` in stack.go
  - [x] Add `NewMemosComponent()` call in `buildStackManager()` in serve.go
  - [x] Landing page shows Memos tile automatically

- [x] **Build and verify** (S)
  - [x] `go build ./...` succeeds
  - [x] Tests pass
  - [x] `go vet` clean

---

## Phase 2: Agent Memory Integration

> Goal: Connect ycode's agent to Memos for persistent long-term memory storage.

- [x] **A1 — Memory API client** (M)
  - [x] Create `internal/memos/client.go` — Go REST client for Memos API
  - [x] CRUD + search operations (CreateMemo, GetMemo, ListMemos, SearchMemos, UpdateMemo, DeleteMemo)
  - [x] Auth support (SignIn, SignUp, bearer token)
  - [x] Tag and content search via CEL filter expressions

- [x] **S4 — Memory tools for agent** (S)
  - [x] `MemosStore` tool — save markdown memos with #tags
  - [x] `MemosSearch` tool — search by content or tag
  - [x] `MemosList` tool — list recent memos with pagination
  - [x] `MemosDelete` tool — delete memos by ID

- [x] **Wiring** (S)
  - [x] Tool specs in `internal/tools/specs.go`
  - [x] Tool handlers in `internal/tools/memos.go`
  - [x] Client injection via `SetMemosClient()` / module-level pattern
  - [x] Registration in `main.go` and `serve.go`

---

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A2 | gRPC client | REST sufficient |
| S5 | Memo relations | Low priority |
| S6 | Attachments | Not core to memory |
| U1/U2 | Multi-user/OAuth | Single-user tool |
| A4 | RSS | Not relevant |
| F3 | Mobile UI | Not relevant |

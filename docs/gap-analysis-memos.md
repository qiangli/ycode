# Gap Analysis: ycode vs Memos

ycode is a pure Go CLI agent harness; Memos is a Go-based lightweight note-taking service (MIT, ~59k stars). Both are Go — making in-process embedding feasible. This analysis focuses on what Memos offers as a **persistent long-term memory backend** for ycode's agent workflows.

---

## 1. Memory / Note Storage

### What ycode Already Has
- 5-layer memory system (working → episodic → semantic → persistent → dream)
- Vector search via embedded store
- Auto-dream compaction (summarization of old memories)
- Flat markdown files for Claude Code auto-memory (`~/.claude/projects/.../memory/`)
- KV store and SQL store abstractions in `internal/storage/`

### Gaps Identified

| # | Feature | Memos | ycode Status | Priority |
|---|---------|-------|-------------|----------|
| S1 | **Structured memo CRUD via API** | Full REST + gRPC API for creating, listing, searching, updating, deleting memos | No external API for memory CRUD — all in-process | High |
| S2 | **Web UI for browsing/editing memories** | Rich React frontend with timeline, editor, search, tags | No UI for memory browsing — only CLI dump | High |
| S3 | **SQLite-backed persistence** | SQLite with WAL mode, migrations, multi-driver support (MySQL, Postgres) | Vector store uses separate embedding DB; no unified memo DB | Medium |
| S4 | **Tagging system** | First-class `#tag` support with search/filter | Tags exist in vector metadata but no dedicated UI/API | Medium |
| S5 | **Memo relations** | Parent/child, references between memos | Memory layers are independent; no inter-memory links | Low |
| S6 | **Attachments** | File attachments with S3/local storage | No attachment support in memory system | Low |

---

## 2. API Surface

### What ycode Already Has
- HTTP/WebSocket API server at `/ycode/` for chat sessions
- Internal service layer (`internal/server/`)
- Tool invocation API

### Gaps Identified

| # | Feature | Memos | ycode Status | Priority |
|---|---------|-------|-------------|----------|
| A1 | **REST API for notes** | `/api/v1/memos` — full CRUD, search, filter by tag/visibility/date | No REST API for memory operations | High |
| A2 | **gRPC + Connect RPC** | Protobuf-defined services with gRPC-Gateway | No gRPC services | Low |
| A3 | **MCP server** | Built-in MCP endpoint at `/mcp` for AI tool access | MCP client exists but no MCP server for memory | Medium |
| A4 | **RSS feed** | RSS endpoint for public memos | Not applicable | Low |

---

## 3. Authentication & Multi-User

### What ycode Already Has
- Token-based auth for API server
- Single-user CLI model

### Gaps Identified

| # | Feature | Memos | ycode Status | Priority |
|---|---------|-------|-------------|----------|
| U1 | **User management** | Multi-user with roles (host/admin/user) | Single-user; no user management needed | Low |
| U2 | **OAuth/SSO** | Identity provider support | Not needed for local agent | Low |

---

## 4. Frontend / UI

### What ycode Already Has
- Basic WebSocket chat UI at `/ycode/`
- Proxy landing page with app grid

### Gaps Identified

| # | Feature | Memos | ycode Status | Priority |
|---|---------|-------|-------------|----------|
| F1 | **Note timeline UI** | Chronological feed with editor, search, filters | No memory browsing UI | High |
| F2 | **Markdown editor** | Rich markdown editor with preview | Chat-only input | Medium |
| F3 | **Mobile-responsive** | Full responsive design | Not applicable for dev tool | Low |

---

## 5. Observability / Infrastructure

### Where ycode Is Ahead
- **Full embedded observability stack**: Prometheus, Jaeger, VictoriaLogs, Perses, Alertmanager — Memos has none of this
- **Proxy landing page**: Unified access to all services — Memos is standalone
- **Agent orchestration**: Workers, teams, NATS messaging — Memos is a simple note app
- **Self-healing**: Panic recovery with AI diagnosis — not present in Memos
- **Tool system**: 50+ built-in tools, plugin system, MCP client — Memos has basic MCP only

---

## Summary

| Priority | Count | IDs |
|----------|-------|-----|
| High | 4 | S1, S2, A1, F1 |
| Medium | 3 | S3, S4, A3 |
| Low | 6 | S5, S6, A2, A4, U1, U2, F2, F3 |

**Key insight**: Rather than reimplementing Memos features, the most efficient path is to **embed Memos as a component** in ycode's observability stack. This immediately delivers S1, S2, A1, and F1 (all High-priority gaps) by running the full Memos server behind ycode's reverse proxy at `/memos/`. ycode's agent can then use Memos' REST API to store and retrieve long-term memories.

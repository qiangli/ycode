# Gap Analysis: YCode vs Open Claw

Comparison of Open Claw (`priorart/openclaw`) features against YCode's current implementation, scoped to four domains.

---

## 1. Session and Memory Management

### What YCode Already Has
- JSONL persistence with auto-rotation (256KB, 3 backups)
- Multi-layered memory: working, short-term, long-term, contextual, persistent
- Memory types: user, feedback, project, reference with YAML frontmatter
- Context budgeting with compaction/pruning thresholds (50%/60%/80%)
- Message pruning: observation masking, soft trim, hard clear
- Dream system for auto-consolidation
- Semantic search (chromem-go) and full-text search (Bleve)
- Optional SQLite dual-writer

### Gaps Identified

| # | Feature | Open Claw | YCode Status | Priority |
|---|---------|-----------|--------------|----------|
| S1 | **Session lifecycle state machine** | Tracks idle → processing → waiting with diagnostic events | No lifecycle states | High |
| S2 | **Session search/filtering API** | `session-utils.search.ts` — query sessions by agent, date, content | No search API | Medium |
| S3 | **Subagent session isolation** | Dedicated subagent session handling with isolated transcript files | Subagents share parent session context | High |
| S4 | **Input provenance tracking** | `input-provenance.ts` — tracks message origin (CLI, webhook, API, subagent) | Not tracked | Low |
| S5 | **Per-session model overrides** | `model-overrides.ts` — switch model mid-session | Model set at config level only | Medium |
| S6 | **Session transcript live events** | Listener pattern emitting real-time transcript updates to subscribers | Bus events exist but no transcript-specific stream | Medium |
| S7 | **Memory export with retention** | Configurable retention (days) for memory export | Memory files persist indefinitely | Low |

---

## 2. Internal Builtin Tools/Skills/Agents and Orchestration Workflows

### What YCode Already Has
- 50+ builtin tools across file ops, code intel, git, agent/task management
- Tool registry with permission enforcement (ReadOnly/WorkspaceWrite/DangerFullAccess)
- Category-based concurrency (standard=8, LLM=2, interactive=serialized)
- Agent delegation with configurable depth limits and lineage tracking
- Hierarchical skill discovery (project → home → env)
- Middleware pattern for cross-cutting concerns (metrics, OTEL, permissions)
- MCP tool support via SourcePlugin/SourceMCP
- Hook system for lifecycle events

### Gaps Identified

| # | Feature | Open Claw | YCode Status | Priority |
|---|---------|-----------|--------------|----------|
| T1 | **Plugin system with manifests** | `openclaw.plugin.json` manifests, dynamic loading via jiti, WeakMap metadata | Tools hardcoded or via MCP; no manifest-driven plugin discovery | High |
| T2 | **Tool allowlisting per agent/context** | Per-plugin and per-session tool allowlists | All tools available to all agents (only permission mode gates access) | High |
| T3 | **Lane-based execution scheduling** | Lanes: Main, Cron, Subagent, Nested — prevents concurrency conflicts | Category semaphores exist but no lane isolation | Medium |
| T4 | **Model fallback chains** | `model-fallback.ts` — ordered fallback across providers/models on failure | Single provider, no automatic fallback | High |
| T5 | **Auth profile rotation with cooldown** | Multiple auth profiles per provider, cooldown/rotation on rate limits | Single API key per provider | Medium |
| T6 | **Subagent announcement protocol** | Queued announcements with dedicated protocol for subagent lifecycle | Subagents spawn directly via delegation | Low |
| T7 | **Tool catalog API** | `tool-catalog.ts` — enumerate all tools with metadata for discovery | Tool list available internally but not exposed as API | Low |
| T8 | **Skill installation from workspace** | Workspace manifest drives skill snapshot installation | Skills discovered from filesystem only | Low |

---

## 3. Messaging Queue and Communication

### What YCode Already Has
- Event bus with two implementations: MemoryBus (in-process), NATSBus (distributed)
- Typed events: turn, text.delta, thinking.delta, tool_use, tool.result, permission, etc.
- Ring buffer (1024 events) for replay on reconnect
- Per-subscriber filtering
- NATS subject hierarchy for distributed routing
- Service layer bridging CLI, WebSocket, and NATS transports

### Gaps Identified

| # | Feature | Open Claw | YCode Status | Priority |
|---|---------|-----------|--------------|----------|
| M1 | **Structured diagnostic events** | Global singleton emitting model.usage, session.state, webhook.*, queue.*, diagnostic.heartbeat | No diagnostic event taxonomy; raw bus events only | High |
| M2 | **Outbound delivery queue with persistence** | File-based queue with crash recovery and retry/backoff | No persistent outbound queue | Medium |
| M3 | **System events (ephemeral per-session)** | Ring buffer (max 20), dedup, delivery context merging | Bus has global ring; no per-session ephemeral queue | Medium |
| M4 | **Tool loop detection events** | Structured events for generic_repeat, known_poll_no_progress, ping_pong, global_circuit_breaker | LoopDetector exists but doesn't emit structured events | Medium |
| M5 | **Stuck session detection** | `session.stuck` events with age and queue depth | Not implemented | Medium |
| M6 | **Heartbeat/health events** | `diagnostic.heartbeat` — periodic health snapshots | No periodic health emission | Low |

---

## 4. OTLP/OTEL Integration

### What YCode Already Has
- Full OTEL SDK: traces, metrics, logs via gRPC + file exporters
- Comprehensive metrics: LLM calls, tokens, cost, tool calls, turns, sessions, compaction, file changes
- Conversation audit logging (JSONL + VictoriaLogs)
- Embedded observability stack: Prometheus, Jaeger, VictoriaLogs, AlertManager
- Reverse proxy for unified observability UI
- Pre-built Perses dashboards
- Tool middleware for full I/O capture
- Retention cleanup (default 3 days)
- slog → OTEL bridge

**YCode is significantly more advanced than Open Claw in this domain.** Open Claw has basic OTLP export via a plugin; YCode has a full embedded observability stack.

### Gaps Identified

| # | Feature | Open Claw | YCode Status | Priority |
|---|---------|-----------|--------------|----------|
| O1 | **Diagnostic event → OTEL span mapping** | Maps DiagnosticEventPayload to trace attributes with redaction | Events not mapped as structured OTEL spans (metrics exist, but no event-driven spans) | High |
| O2 | **Sensitive data redaction in OTEL** | `redactSensitiveText()` for tokens, keys, PII in attributes | No redaction layer | High |
| O3 | **Log transport bridge for plugins** | `registerLogTransport()` — plugins can hook into logging pipeline | Logging is internal; no plugin log transport | Low |

---

## Implementation Plan

### Phase 1: Foundation — Session Lifecycle & Diagnostic Events (High Priority)

**Goal**: Establish the infrastructure that other features build on.

1. **S1 — Session lifecycle state machine**
   - Add `SessionState` enum (Idle, Processing, Waiting, Error) to session package
   - Emit state transitions as bus events
   - Track per-session state in service layer

2. **M1 — Structured diagnostic events**
   - Define `DiagnosticEvent` type with categories: model.usage, session.state, tool.loop, queue.lane, heartbeat
   - Add `EmitDiagnostic()` to bus or as a separate diagnostic emitter
   - Wire existing metrics emission points to also emit diagnostic events

3. **O1 — Diagnostic event → OTEL span mapping**
   - Subscribe to diagnostic events in OTEL layer
   - Map events to spans with structured attributes
   - Wire into existing provider's tracer

4. **O2 — Sensitive data redaction**
   - Create `internal/telemetry/redact` package
   - Pattern-based redaction for API keys, tokens, secrets
   - Apply to OTEL attributes and log records

### Phase 2: Orchestration — Tool Scoping & Model Resilience (High Priority)

**Goal**: Improve agent orchestration robustness.

5. **T2 — Tool allowlisting per agent/context**
   - Add `AllowedTools []string` to agent delegation context
   - Filter tool registry per-agent based on allowlist
   - Define default allowlists per agent type (Explore, Plan, etc.)

6. **T4 — Model fallback chains**
   - Define `FallbackChain []ProviderConfig` in config
   - Implement retry-with-fallback in API provider layer
   - Fallback on rate limit (429), server error (5xx), or timeout

7. **S3 — Subagent session isolation**
   - Create child session from parent with shared transcript reference
   - Isolate subagent messages in separate JSONL file
   - Link parent ↔ child sessions for navigation

8. **T1 — Plugin system with manifests**
   - Define `plugin.json` manifest schema (name, version, tools, hooks)
   - Plugin discovery: scan `~/.ycode/plugins/` and project `.agents/ycode/plugins/`
   - Plugin loader: resolve manifest → register tools in registry

### Phase 3: Communication — Queues & Detection (Medium Priority)

**Goal**: Improve reliability and observability of message flow.

9. **M4 — Tool loop detection events**
   - Extend LoopDetector to emit structured bus events with detector type
   - Add ping-pong detection (alternating tool calls)
   - Add global circuit breaker (total tool calls per turn)

10. **M5 — Stuck session detection**
    - Background goroutine checking session age and queue depth
    - Emit `session.stuck` diagnostic event when thresholds exceeded
    - Configurable thresholds (default: 5 min age, 10 queue depth)

11. **M2 — Outbound delivery queue with persistence**
    - File-based queue in `~/.ycode/queue/`
    - Crash recovery: scan pending items on startup
    - Retry with exponential backoff (max 3 attempts)

12. **M3 — Per-session ephemeral system events**
    - Ring buffer (max 20) per session key
    - Deduplication of consecutive identical events
    - Drain/peek API for consumers

### Phase 4: Refinement — Session Features & Config (Medium/Low Priority)

**Goal**: Polish session management and configurability.

13. **S5 — Per-session model overrides**
    - Add `ModelOverride` field to session metadata
    - Service API: `SwitchModel(sessionID, model)` (already partially exists)
    - Persist override in session metadata file

14. **S2 — Session search/filtering API**
    - Search by agent ID, date range, content keywords
    - Index session metadata for fast lookup
    - Expose via service layer

15. **T3 — Lane-based execution scheduling**
    - Define lane types: Main, Cron, Subagent
    - Route incoming work to lanes
    - Prevent concurrent execution within same lane

16. **S6 — Session transcript live events**
    - Emit `transcript.update` bus events on message append
    - Include session key, message ID, role

17. **T5 — Auth profile rotation**
    - Support multiple API keys per provider
    - Rotate on 429/rate limit with cooldown timer
    - Config: `providers[].keys: [key1, key2, ...]`

18. **M6 — Heartbeat events**
    - Periodic goroutine emitting health snapshots
    - Include: active sessions, queue depth, memory usage
    - Configurable interval (default: 30s)

### Deferred (Low Priority / Not Planned)

| # | Feature | Reason |
|---|---------|--------|
| S4 | Input provenance | Low impact; can be added incrementally |
| S7 | Memory retention | Memory files are small; manual cleanup sufficient |
| T6 | Subagent announcement protocol | Current direct delegation works well |
| T7 | Tool catalog API | Internal access sufficient for now |
| T8 | Skill workspace manifests | Filesystem discovery covers current needs |
| O3 | Plugin log transport | No plugin system yet; revisit after T1 |

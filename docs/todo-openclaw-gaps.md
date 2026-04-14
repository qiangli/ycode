# TODO: Open Claw Gap Implementation

Tracking checklist for implementing Open Claw features in YCode.
See [gap-analysis-openclaw.md](./gap-analysis-openclaw.md) for full analysis.

---

## Phase 1: Foundation — Session Lifecycle & Diagnostic Events

> Goal: Establish the infrastructure that later phases build on.

- [x] **S1 — Session lifecycle state machine**
  - [x] Add `SessionState` enum (Idle, Processing, Waiting, Error) to `internal/runtime/session/lifecycle.go`
  - [x] Emit state transitions via onChange callback
  - [x] `LifecycleTracker` with thread-safe state, duration tracking
- [x] **M1 — Structured diagnostic events**
  - [x] Define `DiagnosticEvent` type with categories in `internal/bus/diagnostic.go`
  - [x] Implement `DiagnosticEmitter` with typed helpers (EmitModelUsage, EmitSessionState, EmitToolLoop, EmitSessionStuck, EmitHeartbeat)
  - [x] 6 diagnostic event types: `model.usage`, `session.state`, `tool.loop`, `queue.lane`, `heartbeat`, `session.stuck`
- [x] **O2 — Sensitive data redaction in OTEL**
  - [x] Create `internal/telemetry/redact/` package with pattern-based redaction
  - [x] Detect API keys (Anthropic, OpenAI, AWS, GitHub), bearer tokens, secrets, email PII
  - [x] `Redact()`, `RedactMap()`, `ContainsSensitive()`, `RedactEnvStyle()` methods
- [x] **O1 — Diagnostic event to OTEL span mapping**
  - [x] `DiagnosticBridge` in `internal/telemetry/otel/diagnostic_bridge.go`
  - [x] Subscribes to all 6 diagnostic event types, creates OTEL spans with redacted attributes
  - [x] Wired into existing tracer provider

---

## Phase 2: Orchestration — Tool Scoping & Model Resilience

> Goal: Improve agent orchestration robustness and extensibility.

- [x] **T2 — Tool allowlisting per agent/context**
  - [x] Added `AllowedTools []string` to `DelegationConfig` + `IsToolAllowed()` method
  - [x] `FilteredRegistry` wrapper in `internal/tools/filtered.go`
  - [x] `DefaultAgentAllowlists` in `internal/tools/allowlists.go` for Explore, Plan, Verification
- [x] **T4 — Model fallback chains**
  - [x] `FallbackProvider` in `internal/api/fallback.go`
  - [x] Retry-with-fallback on 429, 5xx, timeout, connection errors
  - [x] Cooldown tracking per provider (60s default)
- [x] **S3 — Subagent session isolation**
  - [x] `ChildSession` in `internal/runtime/session/child.go`
  - [x] Separate JSONL in `{parentDir}/subagents/{childID}/messages.jsonl`
  - [x] `metadata.json` linking parent <-> child, `ListChildSessions()` for navigation
- [x] **T1 — Plugin system with manifests**
  - [x] `Manifest` with `ToolDefinition`/`HookDefinition` in `internal/plugins/manifest.go`
  - [x] `DiscoverManifests()` scans directories for `plugin.json` files
  - [x] `Loader` in `internal/plugins/loader.go` registers tools with namespaced names

---

## Phase 3: Communication — Queues & Detection

> Goal: Improve reliability and observability of message flow.

- [x] **M4 — Tool loop detection events**
  - [x] `EnhancedLoopDetector` in `internal/runtime/conversation/loop_detector_ext.go`
  - [x] Ping-pong detection (alternating A-B-A-B tool calls)
  - [x] Global circuit breaker (configurable max per turn, default 100)
  - [x] Emits structured `diagnostic.tool.loop` events with detector type
- [x] **M5 — Stuck session detection**
  - [x] `StuckDetector` in `internal/runtime/session/stuck_detector.go`
  - [x] Background goroutine checking session age against threshold
  - [x] Emits `diagnostic.session.stuck` events, configurable (default: 30s check, 5 min threshold)
  - [x] `StuckSessions()` for programmatic inspection
- [x] **M2 — Outbound delivery queue with persistence**
  - [x] `DeliveryQueue` in `internal/bus/delivery_queue.go`
  - [x] File-based persistence with crash recovery
  - [x] Retry with exponential backoff (`BackoffDuration()`), configurable max retries (default 3)
- [x] **M3 — Per-session ephemeral system events**
  - [x] `SystemEventQueue` in `internal/bus/system_events.go`
  - [x] Ring buffer (max 20) per session, deduplication of consecutive identical events
  - [x] `Enqueue()`, `Drain()`, `Peek()`, `Clear()`, `Len()` APIs

---

## Phase 4: Refinement — Session Features & Config

> Goal: Polish session management and improve configurability.

- [x] **S5 — Per-session model overrides**
  - [x] `MetadataStore` in `internal/runtime/session/model_override.go`
  - [x] Disk-backed via `session_meta.json` with in-memory cache
  - [x] `SetModelOverride()`, `ClearModelOverride()`, `ModelOverride()` methods
- [x] **S2 — Session search/filtering API**
  - [x] `Search()` in `internal/runtime/session/search.go`
  - [x] Filter by text query, title, time range (After/Before), with pagination (Limit/Offset)
  - [x] Falls back to `GenerateDefaultTitle()` since Title is not persisted in JSONL
- [x] **T3 — Lane-based execution scheduling**
  - [x] `Scheduler` in `internal/runtime/lanes/lanes.go`
  - [x] Lanes: Main, Cron, Subagent — serialize work per lane
  - [x] `Acquire()`, `TryAcquire()`, `IsActive()`, `ActiveWork()`, `Route()` APIs
- [x] **S6 — Session transcript live events**
  - [x] `TranscriptNotifier` in `internal/runtime/session/transcript_events.go`
  - [x] `NotifyMessageAdded()`, `NotifySessionCleared()` emit `transcript.update` bus events
  - [x] Added `EventTranscriptUpdate` to bus constants
- [x] **T5 — Auth profile rotation**
  - [x] `KeyPool` in `internal/api/key_rotation.go`
  - [x] Thread-safe rotation with per-key cooldowns (default 60s)
  - [x] `Current()`, `Rotate()`, `MarkRateLimited()`, `Available()` methods
  - [x] `RotatingProviderConfig` bridges to `ProviderConfig`
- [x] **M6 — Heartbeat/health events**
  - [x] `Heartbeat` in `internal/runtime/health/heartbeat.go`
  - [x] Periodic runtime metrics: goroutines, heap stats, GC info, uptime
  - [x] Custom gauge registration, configurable interval (default 30s)

---

## Deferred (Low Priority / Not Planned)

| ID | Feature | Reason |
|----|---------|--------|
| S4 | Input provenance tracking | Low impact; can be added incrementally |
| S7 | Memory export with retention | Memory files are small; manual cleanup sufficient |
| T6 | Subagent announcement protocol | Current direct delegation works well |
| T7 | Tool catalog API | Internal access sufficient for now |
| T8 | Skill installation from workspace manifests | Filesystem discovery covers current needs |
| O3 | Plugin log transport bridge | No plugin system yet; revisit after T1 |

# Gap Analysis: Orchestration & Workflow — paperclip + openclaw + opencode

Combined analysis across three agentic tools vs ycode. Focus: self-learning, self-healing, self-improving autonomous loop.

## Where ycode Is Stronger

| Area | ycode Advantage | Compared To |
|------|----------------|-------------|
| OpenTelemetry | Pervasive dual-export (gRPC + file), conversation logger, diagnostic bridge, request logger | All three lack built-in OTEL; paperclip has basic activity logging |
| Embedded Ollama | First-class inference engine with auto-restart, health checks, HuggingFace integration | None have embedded inference; all rely on external providers |
| OCI/Podman | Pure-Go embedded Podman, no external binary, container tool pattern, auto-machine provisioning | None have container-native tool execution |
| Self-Healing | Full state machine (diagnose→fix→rebuild→restart), AI-guided healing, mesh integration | Paperclip has process recovery only; openclaw has orphan cleanup; opencode has none |
| Swarm Orchestration | Explicit handoff protocol with cycle detection, context variable passing | Paperclip uses implicit adapters; openclaw uses hierarchical spawn; opencode has basic agent configs |
| Mesh Background Agents | 5-agent mesh (diagnoser, fixer, learner, researcher, trainer) with event bus | None have autonomous background agent mesh |
| Agent Definition DSL | Rich YAML with DAG, flows, guardrails, A2A delegation, AOP advices | Paperclip uses DB config; openclaw/opencode use static code |
| Skill Engine | Auto-evolution, success tracking, weekly decay, failure-based FIX flagging | Paperclip has managed skills; openclaw has static metadata; opencode has file-based discovery |
| Circuit Breaker | Three-state (closed/open/half-open) with cooldown for autonomous loop | Only openclaw has global circuit-breaker (simpler threshold) |
| Exit Control | Dual-layer exit detection (heuristic + explicit signal) with question suppression | None have structured autonomous exit detection |

## Gaps Identified

| ID | Feature | Source Tool | ycode Status | Priority | Effort |
|----|---------|-------------|-------------|----------|--------|
| A1 | Tool-level loop detection with multiple detectors | openclaw | Missing — stall detection is iteration-level only | High | Medium |
| A2 | Subagent orphan recovery with announce queue | openclaw | Partial — liveness classifier detects but doesn't recover | Medium | Small |
| A3 | Wakeup queue with coalescing | paperclip | Missing — autoloop has no durable scheduling | Medium | Medium |
| A4 | Task-flow dual layer (execution + delivery state) | openclaw | Missing — sprint mixes execution and delivery | Low | Large |
| A5 | Redaction-aware feedback trace export | paperclip | Missing — no structured trace bundles | Low | Medium |
| A6 | Worktree merge reconciliation | paperclip | Missing — worktrees exist but no cross-worktree merge | Low | Large |

## Implementation Plan

### Phase 1: A1 — Tool-Level Loop Detection (High)

**Rationale:** The autonomous loop needs per-tool-call loop detection to prevent agents from repeating identical tool calls, bouncing between two tools, or polling without progress. OpenClaw's 5-detector system is production-proven.

**Design:**
- New package: `internal/runtime/loopdetect/`
- 5 detectors: generic-repeat, unknown-tool, poll-no-progress, ping-pong, global-ceiling
- Progressive severity: info → warning → critical
- Deterministic outcome hashing (SHA-256 of tool name + args + result status)
- Configurable thresholds per detector
- Returns structured `DetectionResult` with level, detector kind, message
- Integration point: conversation runtime post-tool-call hook

**Files to create:**
- `internal/runtime/loopdetect/detector.go` — types, registry, composite detector
- `internal/runtime/loopdetect/detectors.go` — individual detector implementations
- `internal/runtime/loopdetect/detector_test.go` — unit tests

### Phase 2: A2 — Subagent Orphan Recovery (Medium)

**Rationale:** When subagents crash or become stranded, the pool should automatically recover them. ycode already has liveness classification; this adds the recovery action.

**Design:**
- Add `RecoverOrphans` method to agentpool
- Uses existing `ScanStale` from LivenessClassifier
- Recovery actions: cancel context for Critical agents, remove Stranded agents, emit OTEL events
- Integration: periodic scan in mesh diagnoser

**Files to modify:**
- `internal/runtime/agentpool/pool.go` — add RecoverOrphans method

### Phase 3: A3 — Wakeup Queue with Coalescing (Medium)

**Rationale:** For scheduled autonomous runs, durable queueing prevents lost wakeups on restart and coalescing prevents thundering herd.

**Design:**
- New file: `internal/runtime/autoloop/wakeup_queue.go`
- In-memory queue (SQLite persistence deferred)
- Coalescing: dedup by (agent, source) within window
- Priority: on_demand > trigger > scheduled
- Max-1-concurrent enforcement per agent

**Files to create:**
- `internal/runtime/autoloop/wakeup_queue.go` — queue implementation
- `internal/runtime/autoloop/wakeup_queue_test.go` — unit tests

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A4 | Task-flow dual layer | Large refactor of sprint system; benefits unclear for single-operator CLI |
| A5 | Redaction-aware traces | Useful for training data but not blocking for autonomous operation |
| A6 | Worktree merge reconciliation | Complex; ycode worktrees are typically short-lived |

## Verification

- `go test -short -race ./internal/runtime/loopdetect/...`
- `go test -short -race ./internal/runtime/agentpool/...`
- `go test -short -race ./internal/runtime/autoloop/...`
- `make build` must pass with all changes

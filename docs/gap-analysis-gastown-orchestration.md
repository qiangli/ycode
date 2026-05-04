# Gap Analysis: Gastown — Agent Orchestration & Workflow

**Tool:** Gastown (Go, MIT license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Gastown |
|------|-------|---------|
| Multi-agent flows | 8 flow types with DAG executor, hierarchical manager | Mayor→Polecat hierarchy only; no DAG or complex flows |
| Agent definitions | YAML with inheritance, AOP advices, guardrails, output schemas | TOML formulas with `extends` — simpler composition, no guardrails |
| Autonomous loop | 5-phase with stagnation detection and MagenticOne replanning | No autonomous loop; polecats run to completion |
| Self-healing (code fixing) | AI-driven error fixing with 7 failure types, protected paths | Witness nudges stalled polecats but no AI-driven code fixing |
| Skill evolution | Outcome tracking, degradation detection, decay scoring | Plugin gates query wisps (no evolution tracking) |
| Prompt assembly | Static/dynamic boundary, JIT discovery, cache-safe sections | gt prime injects full context; no cache optimization |

## Gaps Identified

| ID | Feature | Gastown Implementation | ycode Status | Priority | Effort |
|----|---------|----------------------|--------------|----------|--------|
| A1 | Heartbeat v2: self-reported agent state | Agents self-report state (working/idle/exiting/stuck) + system freshness check (stale threshold 3min). Hybrid model eliminates timer-only false positives. | agentpool tracks status (Spawning/Running/Completed/Failed) set by system only. No agent self-reporting. No freshness-based staleness. | High | Medium |
| A2 | Capacity governor with dispatch limiting | Configurable max_polecats. Flock prevents race conditions during dispatch. Sling context beads separate scheduling metadata from work. | agentpool tracks active agents but does not enforce capacity limits. No dispatch gating. | High | Low |
| A3 | Cascading hook configuration | Base hooks (town-level) + role-specific overrides + rig-specific overrides. Per-matcher merge strategy (same matcher = replace, different = both). | hooks.Registry is flat — single set of handlers. No role/project layered override system. | Medium | Medium |
| A4 | Persistent identity + ephemeral sessions | Agent identity survives session death. Resume via identity lookup without context re-injection. Worktree preserved across sessions. | agentpool creates new AgentInfo per spawn. No identity persistence across sessions. | Medium | Medium |
| A5 | Beads-driven state machine (append-only events) | All state queries derive from immutable beads ledger. No shadow files. Cross-agent visibility. Audit-friendly. | Sprint state persisted as mutable JSON snapshot. Memory uses JSONL (similar principle) but agent coordination uses mutable state. | Medium | High |
| A6 | Witness patrol pattern | Periodic cycle surveys all agent states, detects failures, nudges recovery. Separate from agents being monitored. | Mesh Fixer runs on error events (reactive). No periodic patrol that surveys all agents proactively. | Medium | Low |
| A7 | Formula composition (convoy/workflow/expansion/aspect) | 4 formula types enable step-level composition. Aspect formulas inject cross-cutting concerns. | Skills are monolithic markdown files. No step-level composition or cross-cutting aspect injection. | Low | Medium |

## Implementation Plan

### Phase 1: Self-Reported Agent State (A1)

**Files to modify:** `internal/runtime/agentpool/pool.go`

Add hybrid heartbeat:
1. `ReportState(agentID, state string)` — agents self-report: working/idle/blocked/exiting
2. `lastReportedAt` timestamp on AgentInfo
3. Freshness check: if `time.Since(lastReportedAt) > staleThreshold` → mark stale regardless of reported state
4. Stale threshold configurable (default 3min)
5. System status (Running/Failed) still set by pool; self-reported state is advisory layer

### Phase 2: Capacity Governor (A2)

**Files to modify:** `internal/runtime/agentpool/pool.go`

Add capacity enforcement:
1. `MaxConcurrent int` config on Pool
2. `CanSpawn() bool` — returns false if active count >= MaxConcurrent
3. `WaitForSlot(ctx) error` — blocks until slot available or context cancelled
4. Atomic counter for active agents (already have RWMutex)

### Phase 3: Witness Patrol (A6)

**Files to modify:** `internal/mesh/mesh.go`

Add patrol agent to mesh:
1. New `Patrol` mesh agent that runs on timer (configurable interval, default 2min)
2. Each tick: scan agentpool for stale/stuck agents
3. For stale agents: emit diagnostic event (wakes Fixer if needed)
4. For stuck agents (self-reported "stuck"): emit escalation event
5. Distinct from Fixer: Patrol detects, Fixer fixes

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A4 | Persistent identity | Requires session persistence design; better addressed with session management overhaul |
| A5 | Append-only event ledger | Fundamental architecture change; ycode's mutable state works for current scale |
| A7 | Formula composition | ycode's skill system serves a different purpose (prompt injection vs step execution) |

## Verification

- Unit test: self-reported state updates AgentInfo; freshness check overrides reported state
- Unit test: capacity governor blocks spawn when at limit; allows when slot freed
- Unit test: patrol agent detects stale agents and emits diagnostics
- Integration test: patrol + fixer cooperate on stale agent recovery
- `make build` must pass

# Gap Analysis: Gastown — Task Lifecycle

**Tool:** Gastown (Go, MIT)
**Domain:** End-to-End Task Lifecycle
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Gastown |
|------|-------|---------|
| Autonomous loop | 5-phase with stall detection | No autonomous loop |
| Phase gates | Declarative composable gates | No phase gating |
| Self-healing | AI-driven error fixing | Witness nudges only |
| Circuit breaker | 3-state with cooldown | No circuit breaker |

## Gaps Identified

| ID | Feature | Gastown Implementation | ycode Status | Priority | Effort |
|----|---------|----------------------|--------------|----------|--------|
| L1 | Three-tier monitoring (Witness→Deacon→Boot) | Tier 1: per-rig Witness patrols tmux/agent state. Tier 2: Deacon polls dispatch queue on heartbeat. Tier 3: Boot watchdog decides triage. | Liveness classifier exists but no proactive patrol. No watchdog hierarchy. | High | Medium |
| L2 | Formula-based task decomposition | 4 types: Convoy (parallel+synthesis), Workflow (sequential+deps), Expansion (template), Aspect (multi-view). TopologicalSort for execution order. | Sprint has Milestone→Slice→Task (sequential). No parallel decomposition or formula types. | Medium | High |
| L3 | Merge queue (refinery) | Batch-then-bisect: score MRs by risk/size/age, stack up to 5, test, bisect failures, merge. | Worktree support but no merge queue or batch testing. | Medium | High |
| L4 | Nudge system for stuck agents | Detection→first nudge→Boot intervention→Deacon triage→manual handoff. Nudge queue batches to prevent alarm storms. | Liveness classifier detects but no nudge/escalation pipeline. | Medium | Medium |

## Implementation Plan

### Phase 1: Task monitor with patrol and escalation (L1, L4) — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| L2 | Formula decomposition | Sprint + swarm parallel flows serve similar purpose; formula types add significant complexity |
| L3 | Merge queue | Requires git merge infrastructure; defer until multi-agent PR workflows are active |

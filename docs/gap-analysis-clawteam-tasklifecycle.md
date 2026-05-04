# Gap Analysis: ClawTeam — Task Lifecycle

**Tool:** ClawTeam (Python, MIT)
**Domain:** End-to-End Task Lifecycle
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | ClawTeam |
|------|-------|----------|
| Autonomous loop | 5-phase RESEARCH→PLAN→BUILD→EVALUATE→LEARN | No autonomous loop |
| Self-healing | AI-driven error fixing | Stale worktree cleanup only |
| Circuit breaker | 3-state with cooldown recovery | No circuit breaker |
| Skill system | Evolution, degradation detection | Static keyword-based |

## Gaps Identified

| ID | Feature | ClawTeam Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| L1 | Task dependencies with auto-unblocking | blocked_by chains with cycle detection. On parent completion: scan dependents, remove from blocked_by, auto-transition blocked→pending. | Sprint tasks are sequential. No blocked_by chains or auto-unblocking. | High | Medium |
| L2 | Wave-based task decomposition from contracts | SprintContract (PLAN artifact) → wave ordering → auto-generates blocked_by chains. Wave N blocked by all wave N-1 tasks. | Sprint Milestone→Slice→Task is flat sequential. No wave concept. | High | Low |
| L3 | Per-agent cost tracking | CostStore.report(): provider, model, input/output tokens, cost_cents. Rolling summary cache. Per-agent breakdown. | Token tracking per conversation but no per-task/per-agent cost tracking. | Medium | Low |
| L4 | Task locking with agent ownership | Task lock acquired on transition to in_progress. Checked against spawn registry for agent liveness. Force override available. | No task locking. Sprint tasks have status but no ownership lock. | Medium | Low |

## Implementation Plan

### Phase 1: Task dependencies with auto-unblocking (L1, L2) — see consolidated implementation
### Phase 2: Per-task cost tracking (L3) — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| L4 | Task locking | Useful for multi-agent but ycode's sprint runner is single-threaded; locking adds complexity for no benefit currently |

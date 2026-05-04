# Gap Analysis: Paperclip — Task Lifecycle

**Tool:** Paperclip (TypeScript, MIT)
**Domain:** End-to-End Task Lifecycle
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Paperclip |
|------|-------|-----------|
| Autonomous loop | 5-phase with stall detection | No autonomous loop |
| Self-healing code fixing | AI-driven with 7 failure types | Recovery reassignment only |
| Phase gates | Declarative composable gates | Execution policy (similar but less composable) |
| Circuit breaker | 3-state with cooldown | No circuit breaker |

## Gaps Identified

| ID | Feature | Paperclip Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| L1 | Heartbeat-driven task execution | Timer-based wakeups per agent interval. Assignment-driven wakeups. Run tracking: queued→running→succeeded/failed/timed_out. | Sprint runner is synchronous. No heartbeat-driven scheduling or run-level tracking. | High | Medium |
| L2 | Issue execution policy state machine | Stages with approval gates. Multi-stage review workflow. Monitor policies for scheduled re-checks. Recovery policy (wake_owner). | Sprint has phases but no approval workflow with multiple reviewers. No monitor policies. | Medium | Medium |
| L3 | Routine scheduling with catch-up | Cron-based routines with timezone awareness. MAX_CATCH_UP_RUNS (25) for missed runs. Coalescing prevents duplicates. | Autoloop has iterations but no cron-based scheduling or catch-up. | Medium | Medium |
| L4 | Issue tree with pause holds | Parent-child issue hierarchies. Pause holds suspend subtrees. Status propagation. | Sprint has milestone→slice→task hierarchy but no pause holds. | Low | Medium |

## Implementation Plan

### Phase 1: Run-level tracking (L1) — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| L2 | Execution policy stages | Sprint phase gates serve similar purpose with less complexity |
| L3 | Routine scheduling | Autoloop + cron integration can be added incrementally |
| L4 | Pause holds | Sprint phases already gate advancement; pause holds add complexity |

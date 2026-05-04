# Gap Analysis: OpenFang — Task Lifecycle

**Tool:** OpenFang (Rust, MIT/Apache-2.0)
**Domain:** End-to-End Task Lifecycle
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | OpenFang |
|------|-------|----------|
| Sprint execution | Full state machine with milestones/slices/tasks/budget | No sprint concept |
| Phase gates | Declarative composable gates | No phase gating |
| Autonomous loop | 5-phase with stall detection and replanning | No autonomous loop |

## Gaps Identified

| ID | Feature | OpenFang Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| L1 | Collaborative task queue (post/claim/complete) | Async decoupled agent collaboration: task_post creates work, task_claim assigns to agent, task_complete returns result. Typed status tracking. | Sprint assigns tasks sequentially to single agent. No queue pattern for multi-agent claim-based work. | High | Medium |
| L2 | Workflow engine with step types | Sequential, FanOut (parallel), Collect, Conditional, Loop modes. Variable interpolation. Error modes (Fail/Skip/Retry). | Sprint has sequential slice→task. No FanOut, Conditional, Loop step types. | Medium | High |
| L3 | Per-agent quota/metering | Scheduler check_quota() before dispatch. MeteringEngine tracks cost per agent in SQLite. | Agent pool tracks tokens per agent but no quota enforcement or cost metering. | Medium | Medium |

## Implementation Plan

### Phase 1: Task dependency with claim-based dispatch — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| L2 | Workflow step types | Sprint's sequential model + swarm parallel flows cover most cases; complex step types add significant complexity |
| L3 | Per-agent metering | Token tracking exists; formal quota enforcement is optimization, not architecture gap |

# Gap Analysis: Ruflo — Task Lifecycle

**Tool:** Ruflo (TypeScript, MIT)
**Domain:** End-to-End Task Lifecycle
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Ruflo |
|------|-------|-------|
| Self-healing | AI-driven error fixing with 7 failure types | Stuck detection with domain reassignment |
| Phase gates | Declarative composable gates | No phase gating |
| Sprint state machine | Milestone→Slice→Task with budget | No structured sprint |

## Gaps Identified

| ID | Feature | Ruflo Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| L1 | Task dependency DAG with priority queuing | Tasks form DAG with forward/backward dependency tracking. Priority tiers (critical→low). FIFO within tier. Blocked tasks auto-queued when deps complete. | Sprint tasks are sequential within slices. No dependency DAG, no priority tiers, no auto-unblocking. | High | Medium |
| L2 | Complexity-based task decomposition | Queen coordinator calculates complexity (0-1 scale) from subtask count, deps, type, description length. Decomposes by type (coding: design→impl→test). | Sprint planner is manual. No complexity scoring or type-based decomposition. | Medium | Medium |
| L3 | Agent capability scoring for dispatch | totalScore = capability(0.3) + load(0.2) + performance(0.25) + health(0.15) + availability(0.1). Dynamic adjustment. | Swarm Router has AI-based selection but no numeric scoring with historical performance. | Medium | Medium |
| L4 | Pattern learning from task outcomes | ReasoningBank: RETRIEVE→JUDGE→DISTILL→CONSOLIDATE. Trajectories stored with quality verdicts. | Memory system captures learnings but no structured trajectory→verdict→distill pipeline for task outcomes. | Low | High |

## Implementation Plan

### Phase 1: Task dependency graph — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| L2 | Complexity decomposition | LLM-based decomposition via sprint planner serves same purpose; complexity scoring is optimization |
| L3 | Capability scoring | Swarm Router handles agent selection; numeric scoring is refinement |
| L4 | Pattern learning pipeline | Valuable but high effort; ycode's memory system provides partial coverage |

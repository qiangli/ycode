# Gap Analysis: Ralph Claude Code — Task Lifecycle

**Tool:** Ralph Claude Code (Bash/JS, MIT)
**Domain:** End-to-End Task Lifecycle
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Ralph |
|------|-------|-------|
| Multi-agent coordination | Swarm, DAG, hierarchical manager | Single-agent only |
| Sprint execution | Milestone→Slice→Task with budget | fix_plan.md checkbox list |
| Phase gates | Declarative composable gates | No phase gating |
| Skill system | Evolution, degradation detection | No skill system |

## Gaps Identified

| ID | Feature | Ralph Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| L1 | Multi-source task import | Unified import from Beads, GitHub Issues, PRD files. Format normalization (markdown checkboxes). Priority heuristics (keyword-based). | Sprint tasks defined inline. No external task import. | High | Low |
| L2 | Per-loop metrics tracking | track_metrics(): loop count, calls/hour, tokens/hour, files changed, completion signals. Status.json updated each loop. Progress.json during execution. | Autoloop tracks iteration results (score, tasks) but no per-iteration metrics file. No live progress tracking. | Medium | Low |
| L3 | Productive timeout detection | On timeout: check if agent made file changes. If productive → success path. If idle → error. Prevents discarding useful partial work. | Bash exec has timeout but binary success/fail. No productive timeout handling in agentexec. | Medium | Low |
| L4 | Log rotation | 10MB threshold rotation. Status file cleanup. History maintenance (last 50 transitions). | No log rotation for loop/sprint execution. | Low | Low |

## Implementation Plan

### Phase 1: Task source interface (L1) — see consolidated implementation
### Phase 2: Productive timeout (L3) — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| L2 | Per-loop metrics | Autoloop IterationResult already captures metrics; file persistence is incremental |
| L4 | Log rotation | Standard operational concern; not architecture gap |

# Gap Analysis: Agent Orchestrator — Task Lifecycle

**Tool:** Agent Orchestrator (TypeScript, MIT)
**Domain:** End-to-End Task Lifecycle
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Agent Orchestrator |
|------|-------|-------------------|
| Autonomous loop | 5-phase RESEARCH→PLAN→BUILD→EVALUATE→LEARN with stall detection | No autonomous loop |
| Sprint execution | Milestone→Slice→Task with phase gates and budget tracking | No structured task decomposition |
| Self-healing | AI-driven error fixing with rebuild/restart | Session recovery validator only |
| Phase gates | Declarative gates (AllTasksComplete, ScoreThreshold, Budget) | No phase gating |

## Gaps Identified

| ID | Feature | AO Implementation | ycode Status | Priority | Effort |
|----|---------|-------------------|--------------|----------|--------|
| L1 | Canonical run tracking with triple-state decomposition | Per-run: session.state + pr.state + runtime.state. Status transitions logged with reasons. Metadata persisted per-session. | Sprint tracks task status (Pending/Running/Completed/Failed) but no per-run tracking with separate session/git/runtime states. | High | Medium |
| L2 | Reaction engine for CI/review events | Pluggable reactions on CI failure, review comments, stuck, needs-input. Escalation budget persists across status oscillation. | No event reactions. Autoloop has stagnation detection but no CI/review awareness. | Medium | Medium |
| L3 | Issue tracker integration (GitHub/Linear/GitLab) | Tracker plugin interface: getIssue(), listIssues(). Three implementations. Issues become sessions. | No issue tracker integration. Sprint tasks defined inline. | Medium | Medium |
| L4 | PR lifecycle tracking | SCM plugin detects PR creation, enriches with CI/review status, auto-merge on approval. | No PR tracking. Worktree support exists but no PR awareness. | Medium | Medium |

## Implementation Plan

### Phase 1: Task run tracker — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| L3 | Issue tracker plugins | Adds external dependency; ycode's sprint model works for now |
| L4 | PR lifecycle | Useful but requires SCM integration; defer until git workflow matures |

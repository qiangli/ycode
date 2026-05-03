# Gap Analysis: MetaGPT — Agent Orchestration & Workflow

**Tool:** MetaGPT (Python multi-agent framework with Team/Role SOP orchestration)
**Source:** `priorart/metagpt/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | MetaGPT |
|------|-------|---------|
| DAG with FanOut | Dynamic map-reduce with topological sort, conditional nodes | ActionGraph has DAG but no fan-out parallelism |
| Termination algebra | Composable MaxTurns/TextMatch/Timeout with AND/OR operators | Basic timeout only |
| Handoff with cycle detection | Linear cycle detection, context variable merging | No cycle detection in role transitions |
| Flow composition | 8 flow types (sequence, chain, parallel, loop, fallback, choice, DAG, router) | Role state machine only |
| Self-healing | FailureType categorization, state machine, escalation policies | No autonomous error recovery |
| Skill evolution | Success rate tracking, decay-based obsolescence | Static tool registry |
| Stall detection & replanning | StallDetector with dual-loop replan (new from AutoGen) | No stall detection |
| Cost tracking | CostTracker with LLM call budgets (new from ADK) | Cost management exists but no per-invocation budget enforcement |
| Scheduling | CronRegistry with enable/disable | None |

## Gaps Identified

| ID | Feature | MetaGPT Implementation | ycode Status | Priority | Effort |
|----|---------|----------------------|--------------|----------|--------|
| A1 | Quality-gate feedback loops | Engineer→CodeReview→FixBug pattern: action output feeds into review action, review can accept or reject, rejection triggers fix cycle with bounded retries | ycode has no built-in review/fix/retry cycle abstraction. Quality gates exist only in sprint runner's two-stage review. | High | Medium |
| A2 | Role state templates | Predefined next-action suggestions based on message content patterns. Reduces LLM overhead for state transitions. | ycode agents use free-form LLM reasoning for next steps; no state template optimization | Low | Medium |

## Implementation Plan

### Phase 1: Quality Gate for Workflows (A1)

**Files to create:**
- `internal/runtime/agentdef/quality_gate.go` — review/fix feedback loop abstraction

**Design:**
- `QualityGate` struct with `Reviewer func(output string) (passed bool, feedback string, err error)` callback
- `MaxRetries int` (default 3) for bounded fix cycles
- Integrates with DAG nodes: when a node has a QualityGate, its output is passed to the reviewer. If rejected, the node re-executes with the feedback appended to its prompt.
- Pattern: Execute → Review → (Accept | Reject+Feedback → Re-execute → Review → ...)
- Inspired by MetaGPT's Engineer→CodeReview→FixBug and also applicable to sprint runner tasks

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A2 | Role state templates | Low priority; ycode's LLM-based reasoning is more flexible. Templates would be a performance optimization, not a capability gap. |

## Verification

- `make build` passes with no errors
- Unit tests for quality gate with accept/reject/retry scenarios
- Existing DAG and flow tests continue to pass

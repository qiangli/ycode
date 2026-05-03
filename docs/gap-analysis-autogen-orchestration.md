# Gap Analysis: AutoGen — Agent Orchestration & Workflow

**Tool:** AutoGen (Python multi-agent framework with MagenticOne orchestrator)
**Source:** `priorart/autogen/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | AutoGen |
|------|-------|---------|
| DAG with FanOut | Parallel map-reduce execution with SplitOn/JoinWith/MaxParallel | DiGraph with conditional edges but no dynamic fan-out |
| Hook system | General-purpose pre/post tool-use interception with pattern matching | No hook system; tool execution inline within agents |
| Skill engine | Auto-selection, success tracking, evolution flagging, weekly decay | Static tools with no adaptation |
| Self-healing | FailureType categorization, state machine, escalation policies | No autonomous error diagnosis/repair |
| Git integration | Worktree isolation, commit conventions, PR workflows | Encourages shell commands but no native git workflow |
| Agent pool | Per-agent metrics, status lifecycle, AgentWait coordination | Distributed via pub/sub but no centralized visibility |
| Scheduling | CronRegistry with enable/disable, interval parsing | No scheduling primitives |

## Gaps Identified

| ID | Feature | AutoGen Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| A1 | Composable termination conditions | StopMessage, MaxMessage, TextMention, TimeoutTermination with AND/OR composition operators. Stateful with explicit reset. | ycode has no reusable termination condition system; stop logic is ad-hoc per workflow | High | Medium |
| A2 | Stall-triggered replanning | MagenticOne's dual-loop: outer (task/facts/plan ledger) + inner (progress tracking). Detects n_stalls, triggers replanning when stuck by refreshing facts and plan from conversation history | ycode autoloop has stagnation detection but no structured ledger-based replanning | High | Medium |
| A3 | Ledger-based orchestration | Structured JSON oracle state (task/facts/plan) + progress ledger. LLM reads ledger + thread, outputs JSON decisions. Reduces hallucination. | ycode swarm uses implicit agent reasoning without structured intermediate state | Medium | High |

## Implementation Plan

### Phase 1: Composable Termination Conditions (A1)

**Files to create:**
- `internal/runtime/agentdef/termination.go` — termination condition types and composition

**Design:**
- `TerminationCondition` interface: `Check(event AgentEvent) *TerminationResult`
- Built-in conditions: `MaxTurns(n)`, `TextMatch(pattern)`, `StopMessage()`, `Timeout(duration)`
- Composition: `And(conditions...)`, `Or(conditions...)` returning new TerminationCondition
- Stateful: conditions track internal state (turn count, etc.) with `Reset()` method
- Integrates with DAG executor and swarm orchestrator via optional field

### Phase 2: Stall Detection with Replanning (A2)

**Files to modify:**
- `internal/runtime/autoloop/loop.go` — add structured stall tracking and replan trigger
- `internal/runtime/autoloop/stall.go` — new file for stall detector with configurable thresholds

**Design:**
- `StallDetector` tracks consecutive turns with no progress (output similarity, no new files, same errors)
- On max stalls: emit replan signal that triggers facts/plan refresh from conversation
- Configurable: `MaxStalls` (default 3), `ProgressMetrics` (output diff, file changes, test results)
- Integrates with existing autoloop stagnation detection, extending it with structured response

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A3 | Ledger-based orchestration | High effort; ycode's existing planner and swarm orchestrator cover most use cases. Ledger pattern is a refinement for interpretability. |

## Verification

- `make build` passes with no errors
- Unit tests for termination condition composition (AND/OR/Reset)
- Unit tests for stall detector thresholds
- Existing autoloop and DAG tests continue to pass

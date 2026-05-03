# Gap Analysis: LangGraph — Agent Orchestration & Workflow

**Tool:** LangGraph (Python framework for stateful multi-agent systems)
**Source:** `priorart/langgraph/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | LangGraph |
|------|-------|-----------|
| Handoff-first orchestration | Explicit `DetectHandoff()`, context variable propagation, cycle detection, max-handoff limits | Implicit graph transitions only |
| Skill system | Auto-selection via semantic matching, success rate tracking, decay-based obsolescence, evolution signals | No skill concept — tools are static |
| Self-healing | FailureType categorization, state machine (diagnosing→fixing→rebuilding→restarting), escalation policies | Only retry with backoff; no autonomous repair |
| Diagnostic mesh | Background passive monitoring, tool degradation detection, latency/error/token-waste diagnostics | Active-only execution |
| Sprint execution | Task decomposition with acceptance criteria, two-stage review, attempt tracking | No structured task management |
| Agent pool & lifecycle | Per-agent metrics, status lifecycle, AgentWait for inter-agent coordination | No multi-agent visibility |
| Workflow phases | RESEARCH→PLAN→BUILD→EVALUATE→LEARN with stagnation detection | Generic loops only |
| Scheduling | CronRegistry with enable/disable, interval parsing | No scheduling primitives |
| Git integration | Worktree isolation, commit conventions, PR workflows | None |

## Gaps Identified

| ID | Feature | LangGraph Implementation | ycode Status | Priority | Effort |
|----|---------|--------------------------|--------------|----------|--------|
| A1 | Structured streaming modes | 6 stream modes: values, updates, checkpoints, tasks, debug, messages. Clients subscribe to specific event types for dashboards | ycode has event emitter but no typed stream modes for external consumers | Medium | Medium |
| A2 | Interrupt DSL with action types | `HumanInterrupt` with typed actions: approve, edit, ignore, accept. Structured pause/resume protocol | ycode uses hook-based gates; no typed interrupt protocol for human-in-the-loop | Low | Medium |
| A3 | Send() fan-out for dynamic parallelism | `Send()` spawns multiple node instances with different state slices for map-reduce patterns | ycode DAG has static parallel layers; no dynamic fan-out with per-instance state | Medium | Medium |
| A4 | Per-node timeout policies | Distinct idle_timeout + run_timeout per node; GraphRecursionError | ycode has EffectiveTimeout() per agent but no idle vs run distinction | Low | Low |

## Implementation Plan

### Phase 1: Structured Stream Events (A1)

**Files to modify:**
- `internal/runtime/conversation/runtime.go` — emit typed stream events during conversation loop
- `internal/runtime/conversation/stream.go` — new file defining stream event types and subscriber interface

**Design:**
- Define `StreamEvent` enum: `EventToolStart`, `EventToolEnd`, `EventModelStart`, `EventModelDelta`, `EventModelEnd`, `EventStateUpdate`, `EventCheckpoint`
- Add `StreamMode` filter on subscriber registration (values, updates, tools, debug)
- Wrap existing event emitter with typed events; backward compatible

### Phase 2: Dynamic Fan-Out in DAG (A3)

**Files to modify:**
- `internal/runtime/agentdef/dag.go` — add `Send` operation for dynamic node spawning
- `internal/runtime/agentdef/definition.go` — extend node definition with fan-out config

**Design:**
- Add `FanOut` field to DAG node: specifies a function that returns N state slices
- Executor spawns N instances of target node, collects results, merges via reducer
- Integrates with existing topological sort; fan-out nodes treated as parallel layer

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A2 | Interrupt DSL | ycode's hook system covers the use case; typed interrupts are a UX refinement, not a capability gap |
| A4 | Per-node idle vs run timeout | Low priority; Go's context cancellation handles the common case |

## Verification

- `make build` passes with no errors
- Unit tests for stream event types and fan-out execution
- Existing conversation loop tests continue to pass

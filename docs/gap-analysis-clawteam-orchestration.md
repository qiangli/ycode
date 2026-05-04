# Gap Analysis: ClawTeam — Agent Orchestration & Workflow

**Tool:** ClawTeam (Python + TypeScript, MIT license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | ClawTeam |
|------|-------|----------|
| Multi-agent flows | 8 flow types with DAG executor | Leader-worker only; no DAG or complex flow topologies |
| Agent definitions | YAML with inheritance, AOP advices, guardrails, output schemas, triggers | CLI-driven coordination via injected prompts; no agent definition language |
| Autonomous loop | 5-phase RESEARCH→PLAN→BUILD→EVALUATE→LEARN with stagnation detection | No autonomous loop |
| Self-healing | AI-driven error fixing, 7 failure types | Stale worktree cleanup only; no AI-driven recovery |
| Skill evolution | Outcome tracking, degradation detection, decay scoring | Static keyword-based auto-selection; no evolution |
| Sprint execution | Full state machine with milestones/slices/tasks/budget | No sprint concept |
| Background agents | Mesh: Diagnoser, Fixer, Learner, Researcher | No background agents |

## Gaps Identified

| ID | Feature | ClawTeam Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| A1 | Phase gates with artifact requirements | PhaseState machine (DISCUSS→PLAN→EXECUTE→VERIFY→SHIP). Gates: ArtifactRequiredGate (block until output exists), AllTasksCompleteGate, HumanApprovalGate. Declarative, pluggable. | Sprint has phases (Plan→Execute→Complete→Reassess→Validate→Done) but no declarative gate system. Phase transitions are code-driven, not gate-driven. | High | Medium |
| A2 | Workspace context injection | Automatically detects file overlaps between agents, injects recent teammate commits, conflict warnings, and dependency context into prompts. Prevents silent merge conflicts. | Prompt builder has JIT instruction discovery and project context but no cross-agent overlap detection or teammate change injection. | High | Medium |
| A3 | Transport abstraction (file + P2P) | Swappable messaging backends: file-based (default), ZeroMQ P2P with automatic fallback. Extensible to Redis, NATS. | Swarm Mailbox is in-process only. No file-based or network transport for cross-process agent communication. | Medium | Medium |
| A4 | CLI-as-coordination-language | Agents receive bash commands in prompts (clawteam task list, clawteam inbox send). Works with ANY CLI agent without SDK integration. | Swarm uses Go function calls; requires agent to be in same process. External agents need adapter interface. | Medium | Low |
| A5 | Event bus with plugin architecture | EventBus (emit/subscribe): BeforeWorkerSpawn, AfterTaskUpdate, TaskCompleted, PhaseTransition, AgentIdle, WorkerCrash. Plugins via entry_points + config + local dirs. | hooks.Registry supports tool events (PreToolUse, PostToolUse). No agent lifecycle events (spawn, idle, crash) in hook system. | Medium | Medium |
| A6 | Message throttling for duplicate suppression | DefaultRoutingPolicy throttles same-pair messages (30s default). Prevents message storms between chatty agents. | Swarm Mailbox is FIFO with no dedup or throttling. | Low | Low |

## Implementation Plan

### Phase 1: Declarative Phase Gates (A1)

**Files to create:** `internal/runtime/sprint/gates.go`

Implement gate system:
1. `PhaseGate` interface: `CanAdvance(state *SprintState) (bool, string)`
2. Built-in gates: `ArtifactExistsGate` (file/output must exist), `AllTasksCompleteGate`, `ScoreThresholdGate` (eval score >= N)
3. Gates are declarative: defined per-phase in sprint config, not hardcoded
4. Sprint phase transition checks all gates before advancing; returns blocking reason if any fail

### Phase 2: Workspace Context Injection (A2)

**Files to create:** `internal/runtime/agentpool/context.go`

Cross-agent context detection:
1. Track files modified per agent (already in AgentInfo via tool use tracking)
2. On agent start: scan other active agents' modified files for overlap
3. Inject warnings into prompt: "Agent X is also modifying file.go (lines 50-80)"
4. Include recent changes from other agents' git branches if in worktree mode

### Phase 3: Agent Lifecycle Events (A5)

**Files to modify:** `internal/runtime/hooks/registry.go`

Add lifecycle event types:
- `AgentSpawned`, `AgentCompleted`, `AgentFailed`, `AgentIdle`
- `PhaseTransition` (from sprint)
- Fire through existing hook registry; plugins can subscribe

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A3 | Transport abstraction | ycode's multi-project server already provides cross-process coordination; file/ZeroMQ transport adds complexity for edge case |
| A4 | CLI-as-coordination | ycode's adapter interface already supports external agents; CLI commands are a UX choice, not architecture gap |
| A6 | Message throttling | Low priority until swarm mailbox handles high message volumes |

## Verification

- Unit test: phase gate blocks advancement when artifact missing; advances when present
- Unit test: composite gates (AND/OR) evaluate correctly
- Unit test: workspace context detects file overlaps between two agents
- Unit test: agent lifecycle hooks fire on spawn/complete/fail
- `make build` must pass

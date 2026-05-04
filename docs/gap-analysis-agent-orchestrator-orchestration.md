# Gap Analysis: Agent Orchestrator — Agent Orchestration & Workflow

**Tool:** Agent Orchestrator (TypeScript/pnpm monorepo, MIT license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Agent Orchestrator |
|------|-------|-------------------|
| Multi-agent flow types | 8 flow types (Sequence, Chain, Parallel, Loop, Fallback, Choice, DAG, Router) | Linear session lifecycle only (spawning→working→pr_open→merged→done) |
| Autonomous loop | 5-phase RESEARCH→PLAN→BUILD→EVALUATE→LEARN with stagnation detection | No autonomous loop; polling-based lifecycle only |
| Self-healing | AI-driven error fixing, 7 failure types, mesh agents | Session recovery validator only (no error fixing) |
| Background agents | Mesh: Diagnoser, Fixer, Learner, Researcher with event bus | No background agents |
| Skill evolution | Auto-selection, outcome tracking, degradation detection | Plugin detection (binary availability check) — no evolution |
| Sprint execution | State machine with retry, budget, resumable milestones/slices/tasks | No sprint concept |
| Agent definitions | YAML-based with inheritance, AOP advices, guardrails, output schemas | Plugin interfaces only, no agent definition language |

## Gaps Identified

| ID | Feature | Agent Orchestrator Implementation | ycode Status | Priority | Effort |
|----|---------|----------------------------------|--------------|----------|--------|
| A1 | Canonical lifecycle with decoupled states | Triple state machine: session.state + pr.state + runtime.state. Each dimension transitions independently. Enables partial recovery. | agentpool has simple AgentStatus (Spawning→Running→Completed/Failed). No PR or runtime state decomposition. | High | Medium |
| A2 | Activity signal classification with fallback chain | Multi-source: native API → JSONL entry → activity-signal classifier → age-based decay. 6 states (active/ready/idle/waiting_input/blocked/exited). Decay demotes only. | agentpool tracks status + tool name but no freshness classification, no fallback chain, no age-based decay. | High | Medium |
| A3 | Persistent reaction keys | CI-failed escalation budget survives status transitions. Prevents oscillation reset. 2 consecutive CI passes required to reset tracker. | No reaction persistence across status changes. autoloop has stagnation detection but resets on phase change. | Medium | Low |
| A4 | Session recovery classification | 8-point rubric: runtime probe, process probe, workspace existence, activity state, metadata status. Classification: live/dead/partial/unrecoverable. Actions: recover/cleanup/escalate/skip. | selfheal handles errors but no multi-point session health rubric. No partial/unrecoverable classification. | Medium | Medium |
| A5 | PATH wrapper hooks for agent-agnostic interception | Shell wrappers prepend ~/.ao/bin to PATH. Intercepts gh/git commands transparently for any CLI agent. Write-through caching for read-only commands. | hooks system uses shell commands with JSON protocol. No PATH interception pattern for agent-agnostic tool monitoring. | Low | Medium |
| A6 | Hash-based namespace isolation | SHA-256(config_dir) + session prefix. Zero collisions across multiple checkouts without central registry. | Worktree paths use workflow ID. No hash-based namespacing for multi-checkout scenarios. | Low | Low |

## Implementation Plan

### Phase 1: Canonical Lifecycle States (A1)

**Files to modify:** `internal/runtime/agentpool/pool.go`

Extend `AgentInfo` with decomposed state:
- `SessionState` (spawning/running/paused/completed/failed)
- `WorkState` (idle/exploring/planning/building/testing/evaluating)
- `GitState` (none/branched/committed/pr_open/ci_running/ci_passed/ci_failed/merged)

Each dimension transitions independently. Recovery can target the failed dimension without resetting others.

### Phase 2: Activity Signal Classification (A2)

**Files to create:** `internal/runtime/agentpool/liveness.go`

Implement 6-state liveness classifier:
1. Check native status (from agentpool)
2. Check recent tool activity (from conversation runtime)
3. Check output freshness (time since last tool result)
4. Age-based decay: active(0-30s) → ready(30s-2m) → idle(2m-10m) → stale(10m+)

Decay demotes only; never promotes on inactivity alone.

### Phase 3: Persistent Reaction Keys (A3)

**Files to modify:** `internal/runtime/autoloop/loop.go`

Add `ReactionState` map persisted across iterations:
- Track named escalation budgets (e.g., "build-failed": {count: 3, maxBefore: reset})
- Require N consecutive successes to reset (not just one pass)
- Survives phase transitions; only clears on explicit reset.

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A5 | PATH wrapper hooks | ycode's hook system already handles tool interception; PATH wrappers add complexity for marginal benefit in single-agent CLI |
| A6 | Hash-based namespacing | ycode's worktree paths use workflow IDs which are already unique; useful only if multi-checkout support needed |

## Verification

- Unit tests for lifecycle state decomposition and independent transitions
- Unit tests for liveness classifier with mock timestamps
- Integration test: agent transitions through work states while git state stays independent
- `make build` must pass

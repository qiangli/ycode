# Gap Analysis: OpenCode — Agent Orchestration & Workflow

**Tool:** OpenCode v1.14.33 (TypeScript/Bun, MIT license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | OpenCode |
|------|-------|----------|
| Multi-agent coordination | Swarm orchestrator with handoff protocol, cycle detection (max 10), context variables, architect-editor pattern, hierarchical manager with LLM decomposition | Task-based tree hierarchy only, single subagent at a time, no DAG/mesh |
| Workflow composition | 8 flow types (Sequence, Chain, Parallel, Loop, Fallback, Choice, DAG, Router) with conditional execution | Implicit phases via agent mode selection, no explicit workflow DSL |
| DAG execution | Full DAG executor with topological sorting, concurrent layer execution, variable substitution, conditional nodes | No DAG support |
| Autonomous loop | 5-phase RESEARCH→PLAN→BUILD→EVALUATE→LEARN with stagnation detection and token budget | No autonomous loop |
| Self-healing | AI-driven error fixing with iterative rebuilding, error classification (7 types), escalation policies | No self-healing; relies on user intervention |
| Background mesh agents | Diagnoser (health monitoring), Fixer (autonomous remediation), Learner (pattern consolidation), Researcher (web research on errors) | No background agents |
| Skill evolution | Auto-selection, outcome tracking, degradation detection (<50% success), weekly decay scoring | Static skill discovery, no learning/evolution |
| Sprint execution | State machine (Plan→Execute→Complete/Failed), retry logic, token budget enforcement, resumable state | No sprint/task runner |
| Scheduling | Internal sprint runner, autoloop iterations, mesh agent intervals | Relies on external GitHub Actions |
| Safety guards | Rate-limited fixes (5/hour), per-report max attempts, hourly window reset | No autonomous fix budget |
| Hook system | Pattern-based with priority ordering, tool input modification, permission decisions | Plugin hooks + event bus (comparable but different approach) |

---

## Gaps Identified

| ID | Feature | OpenCode Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| A1 | Doom loop detection (identical tool calls) | Compares last 3 tool parts for same name + identical JSON input; raises permission request | ycode has self-healing but no specific repeated-tool-call detection | Low | Low |
| A2 | Session forking at arbitrary message | Fork session at any message point with ID remapping; parent-child relationships | ycode has worktrees for isolation but not conversation-level forking | Low | Medium |
| A3 | Agent generation via LLM | `Agent.generate()` creates new agent configs from natural language descriptions | ycode agents defined in YAML; no dynamic generation | Low | Low |
| A4 | Snapshot-based undo/revert | File-level snapshots pre-step enable perfect rollback without git commits | ycode uses git worktrees for isolation (stronger but heavier) | Low | Medium |
| A5 | Permission as auth middleware | Unified permission model gates all operations; once/always/reject with session persistence | ycode has 3-tier permission modes + policy engine (comparable, different design) | Low | N/A |

---

## Implementation Plan

**No actionable gaps identified.**

ycode's orchestration system is substantially more advanced than OpenCode's across every dimension. OpenCode uses a simpler task-based hierarchy with implicit phase management through agent modes, which is clean but fundamentally less capable than ycode's multi-flow-type orchestrator with DAG execution, autonomous loops, mesh agents, and AI-driven self-healing.

The items listed above are either:
- Already handled by ycode through different (often stronger) mechanisms (A4, A5)
- Low-priority features that don't add meaningful capability (A1, A2, A3)

---

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A1 | Doom loop detection | ycode's self-healing + mesh diagnoser already covers stuck detection more comprehensively |
| A2 | Session forking | Worktree isolation provides stronger guarantees; session forking adds complexity without clear benefit |
| A3 | Agent generation via LLM | Nice-to-have but not load-bearing; YAML definitions are more predictable |
| A4 | Snapshot undo | Git worktrees already provide this capability with better guarantees |

---

## Verification

N/A — No implementation required.

---

## Summary

OpenCode's orchestration is well-engineered for its scope (single-agent with task delegation, permission-gated phases, plugin hooks) but does not surface any gaps in ycode's significantly more advanced multi-agent orchestration system. ycode's strengths in this domain include explicit handoff protocols, DAG-based workflows, autonomous self-healing mesh, skill evolution, and sprint execution — none of which have counterparts in OpenCode.

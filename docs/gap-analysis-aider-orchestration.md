# Gap Analysis: Aider — Agent Orchestration & Workflow

**Tool:** Aider v0.x (Python, CLI agent, Apache-2.0 license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | Aider |
|------|-------|-------|
| Multi-agent coordination | Swarm orchestrator with handoff protocol, cycle detection, context variables, DAG executor, hierarchical manager | Architect-editor pattern only (2 agents) |
| Workflow composition | 8 flow types (Sequence, Chain, Parallel, Loop, Fallback, Choice, DAG, Router) | Exception-based coder switching (SwitchCoder) |
| Task delegation | Agent pool with metrics, delegation depth tracking (max 3), tool allowlists | Single sub-coder delegation, no depth control |
| Autonomous loop | 5-phase RESEARCH→PLAN→BUILD→EVALUATE→LEARN with stagnation detection | No autonomous loop |
| Self-healing | AI-driven error fixing, error classification (7 types), iterative rebuilding | Reflection loop (max 3) for lint/test/format errors |
| Background agents | Mesh: Diagnoser, Fixer, Learner, Researcher (background goroutines) | No background agents |
| Skill evolution | Auto-selection, outcome tracking, degradation detection, weekly decay | Static command discovery via method naming |
| Sprint execution | State machine with retry, budget enforcement, resumable state | No sprint/task execution |
| Scheduling | Internal sprint runner, autoloop iterations, mesh agent intervals | File watcher (event-driven only) |
| Safety guards | Rate-limited fixes (5/hour), per-report max attempts | Max 3 reflections only |
| DAG execution | Topological sorting, concurrent layers, variable substitution, conditional nodes | No DAG support |

---

## Gaps Identified

| ID | Feature | Aider Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| A1 | AI comment markers in file watcher | Watches for `# AI!` and `# AI?` in code; auto-triggers code/ask action | ycode file watcher polls a prompt file, no inline code comment triggers | Low | Medium |
| A2 | History summarization on format switch | When changing edit format, summarizes old history to prevent format confusion | ycode doesn't switch edit formats mid-session | Low | N/A |
| A3 | Reflection loop with bounded retries | Lint→fix→re-lint up to 3 times; test→fix→re-test; format error→retry | ycode has self-healing (stronger) but lint loop specifically is configurable MaxRetries=2 | Low | N/A |

---

## Implementation Plan

**No actionable gaps identified.**

ycode's orchestration system is fundamentally more advanced: multi-agent swarm with handoff protocol, DAG execution, autonomous loop with stagnation detection, mesh background agents, and AI-driven self-healing all surpass Aider's simpler architect-editor pattern with bounded reflection.

Aider's novel contribution (AI comment markers in watched files) is an interesting UX pattern but not a core orchestration capability. ycode's file watcher and hook system can achieve similar behavior through configuration.

---

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A1 | AI comment markers | UX feature, not orchestration; can be implemented as a hook if desired |
| A2 | Format-switch summarization | ycode doesn't switch edit strategies mid-session; not applicable |
| A3 | Bounded reflection | ycode's lint.go already has MaxRetries=2; self-healing covers broader cases |

---

## Verification

N/A — No implementation required.

# Gap Analysis: Codex — Agent Orchestration & Workflow

**Tool:** OpenAI Codex CLI (Rust, Apache-2.0 license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | Codex |
|------|-------|-------|
| Multi-agent coordination | Swarm orchestrator with handoff protocol, DAG executor, hierarchical manager, context variables | Agent registry with spawn depth limits, agent graph store |
| Workflow composition | 8 flow types (Sequence, Chain, Parallel, Loop, Fallback, Choice, DAG, Router) | Setup → Execution → Completion phases only |
| Autonomous loop | 5-phase RESEARCH→PLAN→BUILD→EVALUATE→LEARN with stagnation detection | No autonomous loop |
| Self-healing | AI-driven error fixing, error classification (7 types), mesh agents | No self-healing |
| Background agents | Mesh: Diagnoser, Fixer, Learner, Researcher | No background agents |
| Skill evolution | Auto-selection, outcome tracking, degradation detection | Static fingerprint-based skill loading |
| Sprint execution | State machine with retry, budget enforcement, resumable state | No sprint runner |

## Gaps Identified

| ID | Feature | Codex Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| A1 | Cryptographic agent identity (Ed25519 + JWT) | Signed assertions for task-scoped agent authorization | ycode has no cryptographic agent identity | Low | Medium |
| A2 | Persistent agent graph topology | BFS traversal of parent/child edges, lifecycle status tracking | ycode has agent pool (metrics) but no persistent topology | Low | Medium |
| A3 | Cloud Tasks scheduling | Google Cloud Tasks for remote work scheduling | ycode has internal sprint runner (stronger for local) | Low | N/A |

## Implementation Plan

**No actionable gaps identified.** ycode's orchestration is substantially more advanced. Codex's cryptographic identity is interesting for multi-tenant scenarios but not needed for ycode's single-user CLI model.

---

## Summary

Codex has excellent engineering (Rust, production-grade) with a clean agent registry, approval routing, and persistent thread stores. However, ycode's orchestration capabilities (swarm, DAG, mesh, autoloop, self-healing) represent a fundamentally more advanced system. No gaps warrant implementation.

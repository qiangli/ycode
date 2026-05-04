# Gap Analysis: Ruflo — Agent Orchestration & Workflow

**Tool:** Ruflo (TypeScript, MIT license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Ruflo |
|------|-------|-------|
| Self-healing | AI-driven error fixing with 7 failure types, protected paths, rebuild/restart | Stuck detection with domain reassignment but no AI-driven fixing |
| Agent definitions | YAML with inheritance (embed), AOP advices, guardrails, output schemas | Hardcoded domain configs in unified coordinator |
| Sprint execution | Full state machine: Plan→Execute→Reassess→Validate with budget tracking | No sprint concept |
| Git integration | Worktree isolation, branch naming, cleanup automation | Documented but lightly implemented |
| Autonomous loop | 5-phase with stagnation detection and MagenticOne-style replanning | No autonomous loop (agents coordinate, not self-improve) |
| Skill system | Disk-based skills with outcome tracking and degradation detection | Skills via plugins with reasoning-bank scoring but no degradation tracking |

## Gaps Identified

| ID | Feature | Ruflo Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| A1 | Domain-based task routing | 5 semantic domains (queen/security/core/integration/support). Tasks route to domain by type. Prevents "generalist gets everything" bottleneck. Capability scoring (0-1) per agent per domain. | Swarm has agent selection via Router (AI-based fallback) but no semantic domain partitioning. Tasks route to agents, not domains. | High | Medium |
| A2 | Multi-strategy recovery escalation | 4-tier: (1) fallback to alternate agent type, (2) switch topology (hierarchical→mesh), (3) decompose task into subtasks, (4) consensus override. Each tier tried in order. | selfheal has single-strategy: diagnose→fix→rebuild→restart. No topology switching or decomposition escalation. | High | Medium |
| A3 | Capability scoring for agent selection | Agents scored 0-1 on task requirements. Dynamic adjustment based on recent success/failure. Prevents misassignment. | Swarm Router selects agents but no numeric capability scoring or dynamic adjustment. agentdef has tool allowlists but no scores. | Medium | Medium |
| A4 | Message bus with priority queues | Per-agent message ordering with priority levels. Fast agents not blocked by slow ones. Integrates with task priority. | Swarm Mailbox has message queue but no priority levels. Messages are FIFO. | Medium | Low |
| A5 | Federation hub for cross-swarm coordination | Cross-swarm messaging, agent discovery across trust boundaries. | No federation. Multi-project support via server but no cross-swarm coordination protocol. | Low | High |
| A6 | Background learning daemons | 12 workers: ultralearn, optimize, consolidate, predict, audit, map, preload, deepdive, document, refactor, benchmark, testgaps. Priority-based scheduling. | Mesh has 4 agents (Fixer, Researcher, Learner, Trainer). Learner covers some of this. | Low | Medium |

## Implementation Plan

### Phase 1: Domain-Based Task Routing (A1)

**Files to modify:** `internal/runtime/swarm/router.go`, `internal/runtime/agentdef/definition.go`

Add domain routing:
1. Define domains in agent definition YAML: `domain: security | core | testing | review | infrastructure`
2. `DomainRouter` groups agents by domain; routes tasks to matching domain first
3. Fallback: if domain has no available agent, try adjacent domains, then AI-based Router
4. Track domain load for balancing

### Phase 2: Multi-Strategy Recovery Escalation (A2)

**Files to modify:** `internal/selfheal/healer.go`

Extend recovery with escalation tiers:
1. **Tier 1**: Current behavior — diagnose→fix→rebuild (agent-level)
2. **Tier 2**: Reassign to different agent type (if in swarm context)
3. **Tier 3**: Decompose task — split into subtasks, dispatch individually
4. **Tier 4**: Escalate to user with diagnostic bundle

Add `RecoveryTier` enum and `EscalationChain` that tries tiers in order, advancing on failure.

### Phase 3: Capability Scoring (A3)

**Files to modify:** `internal/runtime/agentpool/pool.go`, `internal/runtime/swarm/router.go`

Add per-agent capability scores:
1. `CapabilityProfile` map[string]float64 on AgentInfo (domain→score)
2. Initialize from agent definition (static baseline)
3. Adjust dynamically: success +0.05, failure -0.1 (asymmetric to prevent drift)
4. Router uses scores for weighted selection

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A5 | Federation hub | Requires network protocol design; defer until multi-machine agent support is a priority |
| A6 | Background learning daemons | ycode's Mesh already covers core use cases (Fixer, Learner); 12 daemons is over-engineered for CLI agent |

## Verification

- Unit test: tasks route to correct domain; fallback when domain empty
- Unit test: recovery escalation advances through tiers on repeated failure
- Unit test: capability scores adjust on success/failure within bounds [0,1]
- Integration test: swarm with domain-tagged agents routes correctly
- `make build` must pass

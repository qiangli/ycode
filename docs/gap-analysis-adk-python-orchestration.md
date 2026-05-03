# Gap Analysis: ADK-Python — Agent Orchestration & Workflow

**Tool:** Google Agent Development Kit (ADK-Python)
**Source:** `priorart/adk-python/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | ADK-Python |
|------|-------|------------|
| DAG workflows | Topological sort, parallel layers, conditional nodes, FanOut map-reduce | Tree-based hierarchy only (SequentialAgent/ParallelAgent) |
| Termination algebra | Composable MaxTurns/TextMatch/Timeout/StopMessage with AND/OR operators | Binary escalate signal only |
| Tool filtering per node | AllowedTools/DeniedTools whitelisting per DAG node | No per-node tool restriction |
| Handoff cycle detection | Linear cycle detection across agent chains | No cycle detection |
| Stall detection & replanning | StallDetector with dual-loop replan (new from AutoGen analysis) | LoopAgent with max_iterations but no replanning |
| Self-healing | FailureType categorization, state machine, escalation policies | ReflectAndRetryToolPlugin (tool-level only) |
| Skill evolution | Success rate tracking, decay-based obsolescence, evolution signals | Static skill loading via SkillToolset |
| Git integration | Worktree isolation, commit conventions, PR workflows | None |
| Scheduling | CronRegistry with enable/disable | None |

## Gaps Identified

| ID | Feature | ADK Implementation | ycode Status | Priority | Effort |
|----|---------|-------------------|--------------|----------|--------|
| A1 | Invocation cost tracking | _InvocationCostManager with max_llm_calls limit per invocation. Tracks total LLM calls, enforces budget, raises when exceeded | ycode tracks token usage per turn but has no per-invocation LLM call budget enforcement | High | Low |
| A2 | Transfer-to-agent enum constraints | TransferToAgentTool uses JSON schema enum of valid agent names, preventing LLM hallucination of invalid targets | ycode swarm handoff relies on agent name matching without schema-level validation | Medium | Low |
| A3 | Global plugin lifecycle | PluginManager with precedence (plugins > agent callbacks > defaults). Intercepts at agent/model/tool/user-message level | ycode has hooks but no global plugin registry with precedence ordering | Medium | High |

## Implementation Plan

### Phase 1: Invocation Cost Tracker (A1)

**Files to create:**
- `internal/runtime/conversation/cost.go` — cost tracker with LLM call budget enforcement

**Design:**
- `CostTracker` struct with `MaxLLMCalls int`, `LLMCallCount int`, `TotalInputTokens int`, `TotalOutputTokens int`
- `RecordCall(usage api.Usage)` increments counters
- `BudgetExceeded() bool` checks against configured max
- Integrates with Runtime.Turn() — checked before each LLM call
- Configurable via Config (0 = unlimited, matches ADK's pattern)

### Phase 2: Agent Name Enum Validation (A2)

**Files to modify:**
- `internal/runtime/swarm/handoff.go` — validate handoff target against known agent names

**Design:**
- When building handoff tool schema, populate `enum` field with registered agent names
- LLM sees constrained choices instead of free-form string
- Fallback: if agent name not in enum, return error with suggestions

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A3 | Global plugin lifecycle | High effort; ycode's existing hooks system covers most use cases. Plugin precedence is a framework pattern less critical for CLI agent |

## Verification

- `make build` passes with no errors
- Unit tests for cost tracker budget enforcement
- Unit tests for agent name enum validation
- Existing tests continue to pass

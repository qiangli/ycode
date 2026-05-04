# Gap Analysis: Ruflo — External Agent Delegation

**Tool:** Ruflo (TypeScript, MIT license)
**Domain:** Task Delegation to External Agentic Tools
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Ruflo |
|------|-------|-------|
| Container runtime | Embedded Podman | No container support |
| Self-healing | AI-driven error fixing | Stuck detection + domain reassignment only |
| In-process tools | 50+ built-in tools | Delegates to Claude Code MCP tools |

## Gaps Identified

| ID | Feature | Ruflo Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| D1 | Headless CLI agent executor | Spawns Claude Code with `-p` flag as child process. Output captured, parsed as JSON/text. Timeout enforcement (16 min). | No headless external agent execution. Bash exec can run commands but not as managed agent processes. | High | Medium |
| D2 | MCP server for exposing agent operations | 314 MCP tools: agent_spawn, agent_execute, swarm_init, memory_store, task_create. Subprocess management with PID tracking. | ycode has MCP client + server but no tools that spawn/manage external agents via MCP | Medium | Medium |
| D3 | 3-tier model routing | Tier 1: WASM (skip LLM). Tier 2: Haiku (simple). Tier 3: Sonnet/Opus (complex). Cost-aware routing. | Provider supports model switching but no automatic task-complexity routing | Medium | Medium |
| D4 | Federation hub for cross-swarm delegation | Ephemeral agents with TTL, cross-swarm messaging, Byzantine consensus voting. | No federation. Server supports multi-project but no cross-instance delegation. | Low | High |

## Implementation Plan

### Phase 1: Headless execution in external agent executor — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| D3 | 3-tier model routing | Good optimization but requires complexity scoring; defer until agent workload patterns are clear |
| D4 | Federation hub | Requires network protocol; defer until multi-machine support is a priority |

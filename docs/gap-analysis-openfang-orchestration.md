# Gap Analysis: OpenFang — Agent Orchestration & Workflow

**Tool:** OpenFang (Rust, 14 crates, MIT/Apache-2.0 license)
**Domain:** Agent Orchestration & Workflow
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | OpenFang |
|------|-------|----------|
| Multi-agent flows | 8 flow types with DAG executor, hierarchical manager | Workflow engine with sequential/parallel/conditional but no DAG |
| Agent definitions | YAML with inheritance, AOP advices, guardrails, triggers | Agent TOML configs, no inheritance or AOP |
| Autonomous loop | 5-phase with stagnation detection, replanning | No autonomous loop |
| Sprint execution | State machine with milestones/slices/tasks/budget | No sprint concept |
| Self-healing | AI-driven fixing, 7 failure types, protected paths | Context overflow recovery only (4-stage) |
| Skill evolution | Outcome tracking, degradation detection, decay | Skill registry with hot-reload but no outcome-based evolution |
| Git integration | Worktree isolation, branch naming, cleanup | No explicit git worktree lifecycle |

## Gaps Identified

| ID | Feature | OpenFang Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| A1 | Graduated loop detection with ping-pong awareness | SHA-256 hashing of tool+params. Detects identical calls AND alternating A-B-A-B patterns. Poll tool relaxation (shell_exec expected to repeat). Graduated: Warn→Block→CircuitBreak. | LoopDetector exists in conversation runtime but specifics of ping-pong detection and graduated response unclear. | High | Medium |
| A2 | Event-driven trigger engine | Lifecycle/system/memory/custom content triggers. Prompt template with {{event}} substitution. Fire count + max fires (one-shot). Separate from cron scheduling. | Triggers exist in agentdef (regex-based) but no event-driven waking separate from cron. No fire count limits. | High | Medium |
| A3 | A2A agent discovery protocol | Agent Cards at /.well-known/agent.json. Skills, input/output modes, streaming capabilities advertised. Vendor-agnostic interoperability. | No cross-framework agent discovery. External adapters exist but no capability advertisement. | Medium | High |
| A4 | Inter-agent tools (send/spawn/list/kill) | agent_send (sync messaging), agent_spawn (dynamic creation), agent_list, agent_kill as registered tools. Task-local AGENT_CALL_DEPTH prevents recursion (max 5). | Swarm has handoff protocol and mailbox but no registered tools for inter-agent ops. No call depth tracking. | Medium | Medium |
| A5 | Background agent semaphore limiting | Semaphore-limited concurrent LLM calls (max 5) for background agents. Prevents resource exhaustion. ScheduleMode: Continuous/Periodic/Reactive. | Mesh agents run async but no explicit semaphore for concurrent LLM calls. | Medium | Low |
| A6 | Hook blocking vs observe distinction | BeforeToolCall hooks can reject (blocking). AfterToolCall/BeforePromptBuild/AgentLoopEnd are observe-only. Clean separation. | hooks.HookResponse has continue/block actions. Similar capability exists. | Low | N/A |

## Implementation Plan

### Phase 1: Graduated Loop Detection (A1)

**Files to modify:** `internal/runtime/conversation/loop_detector.go` (or create if needed)

Enhance loop detection:
1. SHA-256 hash tool name + serialized params for duplicate detection
2. Track last N tool hashes; detect A-B-A-B ping-pong (window size 4)
3. Allowlist for expected-repeat tools (bash with polling commands)
4. Graduated response: iteration 1 = warn (inject hint), iteration 2 = block (refuse tool), iteration 3 = circuit break (end conversation turn)

### Phase 2: Event-Driven Trigger Engine (A2)

**Files to create:** `internal/runtime/triggers/engine.go`

Implement event trigger system:
- `TriggerDef`: event pattern (lifecycle/system/memory/custom), prompt template, max fires
- `TriggerEngine`: subscribe to bus.Bus events, match patterns, fire agent wakeups
- Separate from cron: triggers are reactive (event-driven), cron is proactive (time-driven)
- Integrate with existing `bus.Bus` in mesh package

### Phase 3: Inter-Agent Call Depth (A4 partial)

**Files to modify:** `internal/runtime/swarm/orchestrator.go`

Add call depth tracking:
- Context-propagated depth counter (not global state)
- Max depth configurable (default 5)
- Depth exceeded → return error with chain trace
- Log chain: A→B→C for debugging

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| A3 | A2A discovery protocol | Requires HTTP server mode; better addressed when ycode's server mode matures |
| A5 | Background semaphore | Low effort but mesh agents don't currently make concurrent LLM calls; implement when parallel mesh agents are added |
| A6 | Hook blocking distinction | ycode's hook system already supports continue/block actions |

## Verification

- Unit test: ping-pong detection with mock tool call sequences
- Unit test: graduated response escalation (warn→block→circuit break)
- Unit test: trigger engine fires on matching events, respects max fires
- Unit test: call depth overflow returns error at max depth
- `make build` must pass

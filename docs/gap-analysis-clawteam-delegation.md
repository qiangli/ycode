# Gap Analysis: ClawTeam — External Agent Delegation

**Tool:** ClawTeam (Python + TypeScript, MIT license)
**Domain:** Task Delegation to External Agentic Tools
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | ClawTeam |
|------|-------|----------|
| A2A protocol | Full HTTP client + server | No A2A support |
| Container runtime | Embedded Podman | No container support |
| In-process tools | 50+ built-in tools | Delegates everything to CLI agents |
| Self-healing | AI-driven error fixing | Stale worktree cleanup only |

## Gaps Identified

| ID | Feature | ClawTeam Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| D1 | Agent CLI adapter with flag injection | NativeCliAdapter detects 9+ agent types, injects agent-specific flags: --dangerously-skip-permissions (Claude), --dangerously-bypass-approvals (Codex), --print/-p (Kimi), -w (Kimi cwd). | No agent-specific flag injection. AgentDef has Process field but no CLI command building. | High | Medium |
| D2 | Spawn backend abstraction | SpawnBackend ABC: spawn(), inject_runtime_message(). Three backends: TmuxBackend, SubprocessBackend, WshBackend. Factory pattern for selection. | No spawn backend abstraction. | High | Medium |
| D3 | Prompt injection protocol | Identity block + workspace context + task spec + coordination protocol + worker loop keepalive. Teaches agents CLI commands for task/inbox management. | No structured prompt injection for external agents. | High | Low |
| D4 | Agent profiles and presets | Reusable named configs: command, model, base_url, auth_env, extra env. Built-in presets for 10+ providers. | AgentDef has model/tools but no CLI-specific profiles with auth env, base_url, etc. | Medium | Low |
| D5 | Tmux readiness detection | Polls tmux capture-pane for prompt indicators (❯, >) or content stabilization. Configurable timeout. | No readiness detection for external agent processes. | Medium | Low |
| D6 | Persistent spawn registry | JSON file tracks spawned agents: backend, tmux_target, pid, command, spawned_at. Zombie detection (>2 hours). | Worker registry tracks state but no process metadata (PID, command, backend type). | Medium | Low |

## Implementation Plan

### Phase 1: Agent preset with CLI metadata (D1, D4) — see consolidated implementation
### Phase 2: Spawn backend abstraction (D2) — see consolidated implementation
### Phase 3: Prompt injection (D3) — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| D5 | Tmux readiness detection | Useful for tmux backend but ycode's primary mode is subprocess; add when tmux backend implemented |
| D6 | Persistent spawn registry | Worker registry can be extended incrementally |

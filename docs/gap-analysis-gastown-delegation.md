# Gap Analysis: Gastown — External Agent Delegation

**Tool:** Gastown (Go, MIT license)
**Domain:** Task Delegation to External Agentic Tools
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Gastown |
|------|-------|---------|
| A2A protocol | Full HTTP client + server | No A2A support |
| Container runtime | Embedded Podman | No container support |
| In-process tools | 50+ built-in tools | Delegates to CLI agents only |
| Self-healing (code) | AI-driven error fixing | Witness nudges but no code fixing |

## Gaps Identified

| ID | Feature | Gastown Implementation | ycode Status | Priority | Effort |
|----|---------|----------------------|--------------|----------|--------|
| D1 | AgentPresetInfo registry | 13+ agent presets: Command, ProcessNames, SessionIDEnv, ResumeFlag, HooksProvider, ConfigDirEnv, ReadyPromptPrefix, ReadyDelayMs. Single source of truth for all agent-specific behavior. | No preset registry. AgentDef has model/tools but no CLI metadata (process names, session env, resume flags). | High | Medium |
| D2 | Session ID persistence and resume | Per-agent SessionIDEnv (CLAUDE_SESSION_ID, etc.) + ResumeFlag (--resume). Session IDs persisted in .runtime/ dir. Prime command detects and signals readiness. | No session continuity for external agents. A2A has session concept but not for CLI agents. | High | Low |
| D3 | Tmux session spawning with env injection | tmux new-session -s <name> -e GT_ROLE=... -e GT_AGENT=... <command>. All env vars inherited by agent and subprocesses. | No tmux integration for agent spawning. Bash exec doesn't provide env injection context. | Medium | Medium |
| D4 | Formula-driven per-leg agent selection | Formulas specify per-leg agent overrides for parallel workflows. Security leg → Claude, performance leg → Codex. | Swarm flow types (parallel, DAG) exist but no per-agent-type routing within a flow. | Medium | Low |
| D5 | Hook installation per agent type | HooksProvider in AgentPresetInfo drives template selection. Separate templates for autonomous vs interactive modes. Role-aware hook installation. | hooks.Registry is flat; no per-agent-type hook templates. | Medium | Medium |
| D6 | Mail system for inter-agent communication | Typed messages (Task, Escalation, Scavenge, Notification, Reply) with priority levels and delivery modes (queue vs interrupt). Beads-backed persistence. | Swarm Mailbox exists but no typed messages, priority, or interrupt delivery. | Low | Medium |

## Implementation Plan

### Phase 1: Agent preset registry with CLI metadata (D1) — see consolidated implementation
### Phase 2: Session ID persistence (D2) — included in preset registry

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| D3 | Tmux integration | Subprocess backend is primary; tmux is secondary |
| D5 | Per-agent hook templates | Can be added incrementally as external agent support matures |
| D6 | Typed mail messages | Swarm Mailbox serves current needs |

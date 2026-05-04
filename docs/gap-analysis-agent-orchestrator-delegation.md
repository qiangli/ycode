# Gap Analysis: Agent Orchestrator — External Agent Delegation

**Tool:** Agent Orchestrator (TypeScript/pnpm monorepo, MIT license)
**Domain:** Task Delegation to External Agentic Tools
**Date:** 2026-05-04

---

## Where ycode Is Stronger

| Area | ycode | Agent Orchestrator |
|------|-------|-------------------|
| A2A protocol | Full HTTP client + server, agent card spec | No A2A protocol |
| Container runtime | Embedded Podman, no external binary | No container support |
| In-process tools | 50+ built-in tools with three-tier execution | Delegates everything to external agents |
| Self-healing | AI-driven error fixing with rebuild/restart | Recovery validator but no code fixing |

## Gaps Identified

| ID | Feature | Agent Orchestrator Implementation | ycode Status | Priority | Effort |
|----|---------|----------------------------------|--------------|----------|--------|
| D1 | Agent preset registry | 6 agent plugins (claude-code, codex, aider, cursor, kimicode, opencode) each implementing Agent interface with getLaunchCommand(), detectActivity(), getEnvironment() | No registry mapping agent types to CLI commands, flags, env vars, readiness detection | High | Medium |
| D2 | Runtime abstraction (tmux/process) | Runtime plugin interface: create(), sendMessage(), getOutput(), isAlive(), destroy(). Two impls: tmux, process. | No spawn backend abstraction. Bash exec exists but not as agent process manager | High | Medium |
| D3 | Activity detection from external agents | Multi-source: native JSONL → AO activity JSONL → terminal output → age-based decay. 6 activity states. | LivenessClassifier has freshness tracking but no external agent output parsing | High | Medium |
| D4 | PATH wrapper hooks for metadata | Shell wrappers in ~/.ao/bin intercept gh/git commands. Write-through caching. Security validation (traversal prevention). | hooks.Registry handles tool events but no PATH interception for external agent monitoring | Medium | Medium |
| D5 | Prompt delivery modes | Inline (-p flag) or post-launch (sendMessage after readiness). Configurable per agent. | No prompt delivery abstraction for external agents | Medium | Low |
| D6 | CleanupStack on spawn failure | LIFO cleanup stack: metadata → workspace → runtime. Ensures no orphaned resources. | No cleanup chain for external agent spawn failures | Medium | Low |

## Implementation Plan

### Phase 1: Agent Preset Registry (D1) — see consolidated implementation

### Phase 2: Spawn Backend Abstraction (D2) — see consolidated implementation

### Phase 3: External Agent Activity Detection (D3) — see consolidated implementation

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| D4 | PATH wrapper hooks | ycode's hook system already intercepts tool use; PATH wrappers add complexity for external agents that may not use git |
| D6 | CleanupStack | Good pattern but ycode's defer-based cleanup is sufficient for current needs |

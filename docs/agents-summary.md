# Agents - Executive Summary

> Agent architecture analysis of 9 prior-art projects compared to ycode's current implementation.
> Generated 2026-04-11.

---

## Projects Surveyed

| Project | Language | Focus | Agents |
|---------|----------|-------|--------|
| [Aider](research/agents/aider.md) | Python | Terminal pair programming | 11 modes |
| [Claw Code](research/agents/clawcode.md) | Rust | CLI agent harness (reference) | Workers/Teams |
| [Cline](research/agents/cline.md) | TypeScript | VS Code extension | Parallel subagents |
| [Codex CLI](research/agents/codex.md) | Rust/TS | OpenAI agent | V2 task trees |
| [Continue](research/agents/continue.md) | TypeScript | IDE extension | Model-capability |
| [Gemini CLI](research/agents/geminicli.md) | TypeScript | Google agent | 5 local + A2A |
| [OpenClaw](research/agents/openclaw.md) | TypeScript | Multi-channel gateway | ACP + sub-agents |
| [OpenCode](research/agents/opencode.md) | TypeScript | AI coding CLI | 4 agents |
| [OpenHands](research/agents/openhands.md) | Python | Dev agent platform | 6 agents |

---

## ycode Current State

**Already implemented:** 6 agent types (Explore, Plan, General-purpose, and others), Worker/Team/Cron systems, Agent tool for delegation, session compaction.

---

## Consolidated Agent Landscape

### Agent Patterns Across Projects

| Pattern | Projects | Description | ycode Status |
|---------|----------|-------------|-------------|
| **Explore/ReadOnly agent** | ycode, OpenCode, OpenHands, Gemini | Read-only codebase exploration | **Done** |
| **Plan agent** | ycode, OpenCode, Cline, Gemini | Read-only planning mode | **Done** |
| **General-purpose agent** | ycode, OpenCode, Gemini | Full-featured delegation | **Done** |
| **Codebase investigator** | Gemini CLI | Deep code analysis, JSON reports | Partial (Explore) |
| **Browser agent** | Gemini CLI, OpenHands | Autonomous web browsing | **Not implemented** |
| **Memory manager agent** | Gemini CLI | Memory persistence management | Partial (memory system) |
| **Architect → Editor delegation** | Aider | Plan then implement with different models | **Not implemented** |
| **V2 task trees + mailbox** | Codex | Hierarchical agent communication | **Not implemented** |
| **ACP/A2A remote agents** | OpenClaw, Gemini CLI | Inter-harness agent protocol | **Not implemented** |
| **Custom agents (user-defined)** | OpenCode, OpenClaw | Config-based agent creation | **Not implemented** |
| **Keyword-triggered agents** | OpenHands | Auto-activate on conversation content | **Not implemented** |

---

## Priority Gap Summary

### P0 - Critical Gaps

1. **Browser agent** - Autonomous web browsing via Chrome. Present in Gemini CLI, OpenHands. Enables web research without manual tool invocation.

### P1 - Important Gaps

2. **Custom agent definitions** - User-defined agents via config files (OpenCode, OpenClaw pattern). Allows project-specific agent personas.
3. **A2A/ACP remote agents** - Inter-harness agent protocol for cross-tool interop.

### P2 - Valuable Enhancements

4. **V2 task trees + mailbox** - Hierarchical agent communication (Codex pattern).
5. **Inter-agent messaging** - Direct communication between spawned agents.
6. **Keyword-triggered agents** - Auto-activate based on conversation content (OpenHands pattern).
7. **Architect → Editor delegation** - Plan with one model, implement with another (Aider pattern).

---

## Implementation Plan

See [research/agents/plan.md](research/agents/plan.md) for the detailed agents implementation plan.

---

## Per-Project Documentation

| Project | Document |
|---------|----------|
| Aider | [aider.md](research/agents/aider.md) |
| Claw Code | [clawcode.md](research/agents/clawcode.md) |
| Cline | [cline.md](research/agents/cline.md) |
| Codex CLI | [codex.md](research/agents/codex.md) |
| Continue | [continue.md](research/agents/continue.md) |
| Gemini CLI | [geminicli.md](research/agents/geminicli.md) |
| OpenClaw | [openclaw.md](research/agents/openclaw.md) |
| OpenCode | [opencode.md](research/agents/opencode.md) |
| OpenHands | [openhands.md](research/agents/openhands.md) |

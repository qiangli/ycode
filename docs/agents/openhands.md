# OpenHands - Agents Analysis

**Project:** OpenHands (formerly OpenDevin)
**Language:** Python (backend) + React (frontend)
**Repository:** All-Hands-AI/OpenHands

---

## Agents / Subagents

### Primary Agents (V0 - Currently Active)
| Agent | Description |
|-------|-------------|
| **CodeActAgent** | Main all-purpose agent: bash, file editing, Python, browsing |
| **BrowsingAgent** | Specialized web browsing and information retrieval |
| **VisualBrowsingAgent** | Screenshot-based browser interaction |
| **ReadOnlyAgent** | Safe exploration: grep, glob, view, think, finish, web_read |
| **LocAgent** | Lines-of-code specialized variant |
| **DummyAgent** | Test/stub agent |

### Microagents (Domain-Specific)
- Keyword-triggered knowledge agents
- Repository-specific agents from `.openhands/microagents/repo.md`
- Auto-loaded per repository context

### V1 Modern Architecture
- Software Agent SDK (separate repo)
- Application server architecture
- Replaces legacy V0 agent system

### Delegation Pattern
- Root task → subtask hierarchy
- State tracking with delegate levels
- Global and local iteration counters
- CodeActAgent → BrowsingAgent delegation

---

## Security & Guardrails (Agent-Related)

| Mechanism | Description |
|-----------|-------------|
| **Action Confirmation** | CONFIRMED/REJECTED/AWAITING_CONFIRMATION per agent action |
| **Runtime sandboxing** | Docker/Local/Remote/Kubernetes isolation per agent |
| **"Almost stuck" detection** | Recovery mechanism when agent is stuck |
| **Delegation tracking** | State tracking with global/local iteration counters |

---

## Notable Patterns

- **Action-Observation loop:** Event-driven execution: Action → Observation → State update
- **Multi-agent state:** Complex delegation with global/local iteration counters
- **Keyword-triggered skills:** Conversation content activates domain expertise
- **Dual architecture:** V0 (stable) + V1 (SDK-based modern) coexist during migration

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Browser agent (BrowseInteractive) | Not implemented | **High** - web research |
| ReadOnlyAgent (safe exploration) | Implemented (Explore agent) | Done |
| Keyword-triggered agents | Not implemented | **Medium** - auto-activation |
| "Almost stuck" detection | Not implemented | **Medium** - recovery |
| Delegation hierarchy tracking | Partial (task tracking) | Low |

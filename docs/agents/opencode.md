# OpenCode - Agents Analysis

**Project:** OpenCode (AI coding agent CLI)
**Language:** TypeScript (Bun runtime)
**Repository:** opencode-ai/opencode

---

## Agents / Subagents

### Primary Agents (2)
| Agent | Description | Mode |
|-------|-------------|------|
| **build** | Full-access development agent (default) | primary |
| **plan** | Read-only planning/analysis agent | primary |

### Subagents (2)
| Agent | Description | Mode |
|-------|-------------|------|
| **general** | General-purpose, parallel execution | subagent |
| **explore** | Fast codebase exploration (read-only tools) | subagent |

### Hidden Agents (3)
| Agent | Description |
|-------|-------------|
| **compaction** | Session compaction |
| **title** | Session title generation (temp: 0.5) |
| **summary** | Session summary generation |

### Custom Agents
User-defined via config with: name, description, permission, model, prompt, temperature, topP, color, options, steps.

---

## Security & Guardrails (Agent-Related)

| Mechanism | Description |
|-----------|-------------|
| **Permission inheritance** | Subagents inherit parent permission level |
| **Doom loop detection** | Detects when agent is stuck in repetitive patterns |
| **External directory protection** | Agents restricted to project boundaries |

---

## Notable Patterns

- **Custom agent definitions:** User-configurable agents via config file
- **Hidden agents:** Internal agents for compaction, titling, summarization
- **Permission inheritance:** Subagents can't exceed parent permissions

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Custom agent definitions (config) | Not implemented | **Medium** |
| Doom loop detection | Not implemented | **Medium** - safety |
| Hidden agents (compaction, title, summary) | Partial (session compaction) | Low |

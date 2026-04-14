# Continue - Agents Analysis

**Project:** Continue (AI-powered IDE extension)
**Language:** TypeScript
**Repository:** continuedev/continue

---

## Agents / Subagents

Continue uses a **model-capability detection** approach rather than explicit agent types:

| Component | Description |
|-----------|-------------|
| **isRecommendedAgentModel()** | Regex-based detection of agent-capable models |
| **Agent-capable models** | Claude 3.7+, GPT-4.1+, o1/o3/o4, DeepSeek-R1, Gemini 2.5+, Grok-4+ |
| **Enhanced tools** | Agent models get `multi_edit` instead of single `edit` |
| **Agent files** | `AGENT.md`, `AGENTS.md`, `CLAUDE.md` loaded as persistent context |

### Legacy Slash Commands
`/commit`, `/review`, `/cmd`, `/draftIssue`, `/onboard`, `/http`, `/share`

---

## Security & Guardrails (Agent-Related)

| Mechanism | Description |
|-----------|-------------|
| **Permission Manager** | Event-based tool permission requests with "remember decision" |
| **Tool Policy Levels** | disabled, allowedWithoutPermission, allowedWithPermission |

---

## Notable Patterns

- **Model-capability detection:** Tools adapt based on model capabilities
- **Messenger-based protocol:** Event-driven async core/UI separation

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Model-capability detection | Not implemented | Low - ycode is model-agnostic |
| Agent files (AGENT.md) | Partial (CLAUDE.md loading) | Low |

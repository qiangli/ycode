# Cline - Agents Analysis

**Project:** Cline (autonomous AI coding VS Code extension)
**Language:** TypeScript
**Repository:** cline/cline

---

## Agents / Subagents

| Component | Description |
|-----------|-------------|
| **SubagentBuilder** | Configures autonomous subagent instances with model overrides and system prompts |
| **SubagentRunner** | Executes up to 5 parallel subagents, tracks token usage |
| **AgentConfigLoader** | Loads agent configurations from files with tool allowlists |
| **Default subagent tools** | Read-only: FILE_READ, LIST_FILES, SEARCH, LIST_CODE_DEF, BASH (readonly), USE_SKILL |

---

## Security & Guardrails (Agent-Related)

| Mechanism | Description |
|-----------|-------------|
| **Tool allowlists** | Subagents limited to approved tool sets |
| **Read-only default** | Subagents default to read-only tool access |
| **Token tracking** | Usage monitoring per subagent |

---

## Notable Patterns

- **Parallel execution:** Up to 5 concurrent subagents for research tasks
- **Model overrides:** Subagents can use different models than parent

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Parallel subagents (up to 5) | Implemented (Agent tool) | Done |
| Tool allowlists per agent | Not implemented | **Medium** - security |
| Model-variant tool definitions | Not implemented | Low - over-engineering for CLI |

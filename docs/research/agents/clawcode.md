# Claw Code - Agents Analysis

**Project:** Claw Code (reference implementation for ycode)
**Language:** Rust
**Repository:** ultraworkers/claw-code

---

## Agents / Subagents

| Component | Description |
|-----------|-------------|
| **Agent tool** | Launches specialized agent tasks with custom prompts |
| **Agent definitions** | `.claw/agents/*.toml` with name, description, model, reasoning_effort |
| **Worker boot system** | State machine: Spawning → TrustRequired → ReadyForPrompt → Running → Finished/Failed |
| **Team coordination** | Parallel subagent teams via TeamCreate |
| **Cron scheduling** | Recurring task execution |

---

## Security & Guardrails (Agent-Related)

| Mechanism | Description |
|-----------|-------------|
| **Permission modes** | Agents inherit permission level from parent |
| **Worker trust gates** | Trust resolution required before worker can execute |
| **Plugin permissions** | Read/Write/Execute per plugin |
| **MCP namespacing** | Server-prefixed tool names prevent conflicts in agent context |

---

## Notable Patterns

- **TOML-based agent definitions:** Declarative agent configuration
- **Worker boot state machine:** Formal lifecycle management
- **Feature flags:** Agent capabilities gated by feature flags

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Worker boot state machine | **Implemented** | Done |
| Team/Cron tools | **Implemented** | Done |
| Agent definitions (.toml) | Not implemented | **Medium** - custom agents |
| Container detection tuning | Basic implementation | Low |

# Gemini CLI - Agents Analysis

**Project:** Google Gemini CLI
**Language:** TypeScript/Node.js
**Repository:** google-gemini/gemini-cli

---

## Agents / Subagents

### Built-in Local Agents (5)
| Agent | Description | Timeout | Max Turns |
|-------|-------------|---------|-----------|
| **generalist** | General-purpose, all tools, non-interactive | 10 min | 20 |
| **codebase_investigator** | Read-only code analysis, low temperature (0.1), structured JSON output | 10 min | 50 |
| **cli_help** | CLI documentation agent | - | - |
| **browser_agent** | Autonomous web browser via Chrome accessibility tree (experimental) | 10 min | 30 |
| **memory_manager** | Long-term memory persistence, scoped workspace | - | - |

### Remote Agents (A2A Protocol)
| Feature | Description |
|---------|-------------|
| **A2A protocol** | Agent-to-Agent execution via URL-based agent cards |
| **Authentication** | OAuth2, API Key, HTTP, Google Credentials |
| **Identity verification** | Hash-based agent acknowledgment |
| **Agent cards** | URL or inline JSON format |

---

## Security & Guardrails (Agent-Related)

| Mechanism | Description |
|-----------|-------------|
| **Subagent scoping** | Policy rules can target specific subagents |
| **Browser security** | Domain allowlist, prompt injection protection for browser agent |
| **Agent timeouts** | Configurable per-agent timeouts (default 10 min) |
| **Turn limits** | Max turns per agent to prevent runaway execution |

---

## Notable Patterns

- **Agent-as-Tool:** Agents callable via `delegate_to_agent` tool
- **ReAct loop:** Agent execution via LocalAgentExecutor with configurable turns
- **A2A protocol:** Remote agent interop with authentication

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Browser agent (Chrome automation) | Not implemented | **High** - unique capability |
| A2A remote agents | Not implemented | **Medium** - future interop |
| Codebase investigator agent | Partial (Explore agent) | Low - similar concept |
| Memory manager agent | Partial (memory system exists) | Low |

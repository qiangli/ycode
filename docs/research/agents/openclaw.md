# OpenClaw - Agents Analysis

**Project:** OpenClaw (multi-channel AI assistant gateway)
**Language:** TypeScript (ESM)
**Repository:** openclaw/openclaw

---

## Agents / Subagents

### Multi-Agent Architecture
| Component | Description |
|-----------|-------------|
| **Agent isolation** | Session key: `agent:<agentId>:main` vs `agent:<agentId>:subagent:<uuid>` |
| **Per-agent config** | Workspace, skills, tool policies, models, timeouts, sandbox |
| **Spawn depth** | Configurable: depth 0 (main) → depth 1 (sub) → depth 2 (sub-sub) |
| **Concurrency** | `maxChildrenPerAgent` (default 5), `maxConcurrent` (default 8) |
| **Auto-archive** | After configurable inactivity (default 60 min) |

### ACP (Agent Client Protocol) Sessions
| Feature | Description |
|---------|-------------|
| **Supported harnesses** | Codex, Claude Code, Cursor, Gemini CLI, OpenCode |
| **Binding modes** | Current-conversation (`--bind here`) or thread (`--thread auto`) |
| **Commands** | `/acp spawn`, `/acp status`, `/acp model`, `/acp cancel`, `/acp close` |

---

## Security & Guardrails (Agent-Related)

| Mechanism | Description |
|-----------|-------------|
| **Session isolation** | Per-agent tool policies, transcript logging |
| **Sandbox inheritance** | Sub-agents must match parent sandbox state |
| **Spawn depth limits** | Configurable maximum nesting depth |
| **Concurrency limits** | maxChildrenPerAgent and maxConcurrent caps |

---

## Notable Patterns

- **ACP runtime:** Interop with external agent harnesses (Codex, Claude Code, Gemini CLI)
- **Thread binding:** Discord thread ↔ agent session binding
- **Auto-archive:** Inactivity-based session cleanup

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| ACP (Agent Client Protocol) interop | Not implemented | **Medium** - future agent interop |
| Spawn depth control | Not implemented | **Medium** - safety |
| Auto-archive | Not implemented | Low |
| Per-agent config (models, timeouts) | Not implemented | **Medium** |

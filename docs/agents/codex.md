# Codex CLI - Agents Analysis

**Project:** OpenAI Codex CLI Agent
**Language:** Rust (codex-rs core), TypeScript (codex-cli wrapper)
**Repository:** openai/codex

---

## Agents / Subagents

Codex has the most sophisticated multi-agent system among surveyed projects:

| Component | Description |
|-----------|-------------|
| **Agent spawning** | V1 (simple) and V2 (task tree, model selection, reasoning_effort) |
| **Agent hierarchy** | Root agent + spawned subagents form task tree |
| **Inter-agent messaging** | send_input (V1), send_message (V2), followup_task (V2) |
| **Fork modes** | FullHistory or LastNTurns for context propagation |
| **Nickname system** | Randomized from configurable candidate lists |
| **Agent status** | Pending, Running, Completed, Failed, Closed, Resumable |
| **Weak references** | Prevents circular memory in agent registry |
| **Batch spawning** | CSV-driven parallel agent creation |

---

## Security & Guardrails (Agent-Related)

| Mechanism | Description |
|-----------|-------------|
| **Guardian Review Agent** | Dedicated LLM reviews every approval request with risk assessment |
| **Execution policy** | Skip/NeedsApproval/Forbidden per command, applied to agent actions |
| **Approval caching** | Session-level caching reduces repeated prompts across agents |

---

## Notable Patterns

- **V2 task trees:** Hierarchical agent communication with mailbox-based messaging
- **Fork modes:** FullHistory vs LastNTurns for different context needs
- **Batch spawning:** CSV-driven parallel agent creation for repetitive tasks
- **Agent lifecycle:** Full status tracking (Pending → Running → Completed/Failed/Closed/Resumable)

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Multi-agent V2 (task trees, mailbox) | Partial (Agent tool exists) | **Medium** - enhanced delegation |
| Inter-agent messaging | Not implemented | **Medium** |
| Batch agent spawning (CSV) | Not implemented | Low |
| Agent lifecycle tracking | Partial (task status) | Low |

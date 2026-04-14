# Aider - Agents Analysis

**Project:** Aider (AI pair programming in the terminal)
**Language:** Python
**Repository:** paul-gauthier/aider

---

## Agents / Subagents

Aider implements a **Coder-as-agent** pattern where each edit format is a specialized agent:

| Agent (Coder Mode) | Purpose | Edit Format |
|---------------------|---------|-------------|
| **AskCoder** | Q&A without changes | `ask` |
| **HelpCoder** | Assistance and help | `help` |
| **ArchitectCoder** | Plan architecture, then delegate to editor | `architect` |
| **ContextCoder** | Identify files to edit (max 3 reflections) | `context` |
| **EditBlockCoder** | Block-based replacement | `editblock` |
| **EditBlockFencedCoder** | Fenced block syntax | `editblock_fenced` |
| **PatchCoder** | Unified diff patches | `patch` |
| **UnifiedDiffCoder** | Unified diff with conflict resolution | `udiff` |
| **WholeFileCoder** | Full file replacement | `wholefile` |
| **Editor*Coder** (3 variants) | Implementation phase editors | varies |

**Delegation pattern:** ArchitectCoder generates a plan, then spawns an EditorCoder to implement it.
**Switching:** `SwitchCoder` exception enables runtime mode transitions with chat history summarization.

---

## Security & Guardrails (Agent-Related)

| Mechanism | Description |
|-----------|-------------|
| **Read-only files** | Files visible to LLM but protected from edits |
| **User confirmation** | Required for new file creation, unadded file edits, destructive ops |
| **Response validation** | Schema checking, fence detection, reflection retries (max 3) |

---

## Notable Patterns

- **Multi-model architecture:** Main model + editor model + weak model
- **Chat history summarization:** LLM-driven compression when switching modes
- **Architect-then-editor:** Two-phase delegation with different model capabilities

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Architect-then-editor delegation | Partial (Agent tool exists) | Low - ycode's Agent tool covers this |
| Multi-model (main/editor/weak) | Not implemented | **Medium** - useful for cost optimization |
| Chat history summarization | Implemented (session compaction) | Done |

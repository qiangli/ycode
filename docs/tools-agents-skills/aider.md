# Aider - Tools, Agents, Skills & Security Analysis

**Project:** Aider (AI pair programming in the terminal)
**Language:** Python
**Repository:** paul-gauthier/aider

---

## Tools (Function Calling)

Aider uses a minimal function-calling approach with only 2 tools, relying instead on edit-format strategies:

| Tool | Description | Parameters |
|------|-------------|------------|
| `write_file` | Create or overwrite files (WholeFileFunctionCoder) | `explanation`, `files[{path, content}]` |
| `replace_lines` | Edit files via block replacement (EditBlockFunctionCoder) | `explanation`, `edits[{path, original_lines, updated_lines}]` |

**Note:** Most editing is done through prompt-based edit formats (not function calling), making Aider unique among the surveyed tools.

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

## Skills / Commands (40+)

| Category | Commands |
|----------|----------|
| **Model Selection** | `/model`, `/editor-model`, `/weak-model`, `/models` |
| **Chat Mode** | `/chat-mode`, `/ask`, `/code`, `/architect`, `/context` |
| **File Management** | `/add`, `/drop`, `/ls`, `/read-only` |
| **Code Analysis** | `/diff`, `/map`, `/map-refresh` |
| **Execution** | `/test`, `/run`, `/commit`, `/lint`, `/git` |
| **Session** | `/clear`, `/undo`, `/reset`, `/load`, `/save` |
| **I/O** | `/copy`, `/paste`, `/copy-context`, `/voice`, `/web`, `/tokens` |
| **Config** | `/settings`, `/multiline-mode`, `/reasoning-effort` |
| **Help** | `/help`, `/exit`, `/quit`, `/report` |

---

## Security & Guardrails

| Mechanism | Description |
|-----------|-------------|
| **File edit permissions** | `allowed_to_edit()` checks file existence, gitignore, chat membership |
| **Read-only files** | Files visible to LLM but protected from edits |
| **User confirmation** | Required for new file creation, unadded file edits, destructive ops |
| **Input validation** | JSON schema, path normalization, symlink prevention |
| **Auto-linting** | Python compile checks, flake8 integration, tree-sitter validation |
| **Response validation** | Schema checking, fence detection, reflection retries (max 3) |
| **Git guardrails** | Auto-commit tracking, dirty commit checks, undo support |
| **SSL verification** | Enabled by default for all HTTP requests |
| **Dry run mode** | Prevents actual file modifications for testing |
| **Overeager/lazy prompts** | Prevents LLM from exceeding scope or producing incomplete code |

---

## Notable Patterns

- **Multi-model architecture:** Main model + editor model + weak model
- **Repository mapping:** Tree-sitter AST for intelligent code summarization
- **Chat history summarization:** LLM-driven compression when switching modes
- **Streaming diffs:** Real-time file change rendering during generation
- **Voice integration:** Whisper-based transcription for voice input

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Architect-then-editor delegation | Partial (Agent tool exists) | Low - ycode's Agent tool covers this |
| Multi-model (main/editor/weak) | Not implemented | **Medium** - useful for cost optimization |
| Repository mapping (tree-sitter AST) | Not implemented | **Medium** - would improve context quality |
| Auto-linting after edits | Not implemented | **Medium** - quality guardrail |
| Voice input | Not implemented | Low - niche feature |
| Chat history summarization | Implemented (session compaction) | Done |
| Streaming diffs | Not implemented | Low - cosmetic |

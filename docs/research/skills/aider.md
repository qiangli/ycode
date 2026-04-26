# Aider - Skills Analysis

**Project:** Aider (AI pair programming in the terminal)
**Language:** Python
**Repository:** paul-gauthier/aider

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

## Security & Guardrails (Skill-Related)

| Mechanism | Description |
|-----------|-------------|
| **Git guardrails** | Auto-commit tracking, dirty commit checks, undo support |
| **Dry run mode** | Prevents actual file modifications for testing |
| **Overeager/lazy prompts** | Prevents LLM from exceeding scope or producing incomplete code |

---

## Notable Patterns

- **Voice integration:** Whisper-based transcription for voice input
- **Rich command set:** 40+ commands organized by logical category

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Auto-linting after edits | Not implemented | **Medium** - quality guardrail |
| Voice input | Not implemented | Low - niche feature |
| `/test` command (test-runner) | Not implemented | **Medium** - useful workflow |

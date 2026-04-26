# Aider - Tools & Security Analysis

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

- **Repository mapping:** Tree-sitter AST for intelligent code summarization
- **Streaming diffs:** Real-time file change rendering during generation

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Multi-model (main/editor/weak) | Not implemented | **Medium** - useful for cost optimization |
| Repository mapping (tree-sitter AST) | Not implemented | **Medium** - would improve context quality |
| Auto-linting after edits | Not implemented | **Medium** - quality guardrail |
| Streaming diffs | Not implemented | Low - cosmetic |

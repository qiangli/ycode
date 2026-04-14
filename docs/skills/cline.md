# Cline - Skills Analysis

**Project:** Cline (autonomous AI coding VS Code extension)
**Language:** TypeScript
**Repository:** cline/cline

---

## Skills

| Component | Description |
|-----------|-------------|
| **Discovery** | Scans `~/.cline/skills/` (global) and `.cline/skills/` (project) |
| **Format** | Subdirectory with `SKILL.md` file, YAML frontmatter (name, description) |
| **Activation** | Via `use_skill` tool, instructions injected into context |
| **Toggles** | Global and project-level skill enable/disable |

### Slash Commands (Built-in)
`/newtask`, `/smol` (`/compact`), `/newrule`, `/reportbug`, `/deep-planning`, `/explain-changes`

### Custom Workflows
File-based from `.cline/workflows/` and `~/.cline/workflows/`, plus remote workflows.

---

## Security & Guardrails (Skill-Related)

| Mechanism | Description |
|-----------|-------------|
| **Skill toggles** | Enable/disable skills at global and project level |
| **Custom rules** | `/newrule` creates persistent guidelines |

---

## Notable Patterns

- **PromptRegistry:** Singleton managing all system prompts with variant system
- **Custom workflows:** File-based workflow definitions beyond skills

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Custom workflows (file-based) | Skills cover this | Done |
| Skill enable/disable toggles | Not implemented | Low |
| `/deep-planning` command | Partial (plan mode) | Low |

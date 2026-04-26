# OpenCode - Skills Analysis

**Project:** OpenCode (AI coding agent CLI)
**Language:** TypeScript (Bun runtime)
**Repository:** opencode-ai/opencode

---

## Skills

| Component | Description |
|-----------|-------------|
| **Format** | `SKILL.md` with YAML frontmatter (name, description) |
| **Locations** | `~/.claude/skills/`, `~/.agents/skills/`, `.opencode/skills/`, `.opencode/plans/`, plugins |
| **Loading** | Bundled with up to 10 related files |
| **Dedup** | First-wins on duplicate names |

---

## Security & Guardrails (Skill-Related)

| Mechanism | Description |
|-----------|-------------|
| **First-wins dedup** | Prevents skill name collisions |
| **External directory protection** | Skill dirs whitelisted for access |

---

## Notable Patterns

- **Multi-source loading:** Skills from user, agents, project, plans, and plugins directories
- **Bundled files:** Skills can include up to 10 related files
- **Plugin system:** Auth plugins (Codex, Copilot, Gitlab, Poe, Cloudflare)

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Bundled skill files (up to 10) | Not implemented | Low |
| Auth plugins (Copilot, Gitlab, etc.) | Not implemented | Low |
| Multi-source discovery | Partial (project + user) | Low |

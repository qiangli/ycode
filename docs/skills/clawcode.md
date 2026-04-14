# Claw Code - Skills Analysis

**Project:** Claw Code (reference implementation for ycode)
**Language:** Rust
**Repository:** ultraworkers/claw-code

---

## Skills

| Component | Description |
|-----------|-------------|
| **Skill tool** | Loads SKILL.md files with frontmatter |
| **Discovery** | `.claw/skills/`, `.codex/skills/`, user-level dirs |
| **Legacy support** | `.commands/` directory format |
| **Shadowing** | Project skills override user skills with same name |

### Slash Commands (70+)
Categories: Session, Workspace/Git, Discovery/Debug, Automation, Plugin, Execution Control, Specialized.

---

## Security & Guardrails (Skill-Related)

| Mechanism | Description |
|-----------|-------------|
| **Config hierarchy** | User → Project → Local with override precedence |
| **Skill shadowing** | Project skills override user skills to prevent hijacking |

---

## Notable Patterns

- **Legacy support:** Backward-compatible `.commands/` directory format
- **Multi-source discovery:** Multiple directories searched with precedence

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| 70+ slash commands | 6 skills implemented | **Medium** - expand library |
| Legacy `.commands/` support | Not applicable | N/A |
| Skill shadowing | Not implemented | **Medium** - security |

# Continue - Skills Analysis

**Project:** Continue (AI-powered IDE extension)
**Language:** TypeScript
**Repository:** continuedev/continue

---

## Skills

| Component | Description |
|-----------|-------------|
| **Locations** | `~/.claude/skills/` (global), `./.claude/skills/` (workspace) |
| **Format** | `SKILL.md` with YAML frontmatter (name, description, version) |
| **Access** | Via `read_skill` tool |
| **Associated files** | Skill directories can contain supporting files |

### Rules System
| Type | Description |
|------|-------------|
| **Always Apply** | Included automatically |
| **Auto Attached** | Matched via globs/regex |
| **Agent Requested** | AI decides when to apply |
| **Manual** | Only when explicitly @mentioned |

---

## Security & Guardrails (Skill-Related)

| Mechanism | Description |
|-----------|-------------|
| **Rules precedence** | Layered rules with clear override order |

---

## Notable Patterns

- **Rules system:** Four-tier rules with different activation modes
- **MCP OAuth:** Full OAuth support for MCP server connections in skill context
- **Edit aggregation:** Tracks user edits for context via EditAggregator

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Rules system (auto-attach, agent-requested) | Partial (CLAUDE.md loading) | **Medium** |
| `/onboard` command | Not implemented | **Medium** - onboarding |
| YAML permission policies | Not implemented | Low - config-based policies exist |

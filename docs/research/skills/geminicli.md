# Gemini CLI - Skills Analysis

**Project:** Google Gemini CLI
**Language:** TypeScript/Node.js
**Repository:** google-gemini/gemini-cli

---

## Skills (11 built-in)

| Skill | Description |
|-------|-------------|
| `async-pr-review` | Asynchronous PR review with GitHub integration |
| `behavioral-evals` | Test/evaluation framework |
| `ci` | CI/CD integration |
| `code-reviewer` | Structured code review analysis |
| `docs-changelog` | Documentation changelog generation |
| `docs-writer` | Documentation writing assistance |
| `github-issue-creator` | Automated GitHub issue creation |
| `pr-address-comments` | Automated PR comment addressing |
| `pr-creator` | Automated PR creation |
| `review-duplication` | Code duplication detection |
| `string-reviewer` | Text content review |

### Skill Architecture
- Precedence: Built-in < Extensions < User < Workspace
- Activation via `activate_skill` tool
- Script-based or tool-based implementation

---

## Security & Guardrails (Skill-Related)

| Mechanism | Description |
|-----------|-------------|
| **Skill precedence** | Workspace skills override user/built-in (prevents hijacking) |
| **Content filtering** | .gitignore, .geminiignore for skill file discovery |

---

## Notable Patterns

- **11 built-in skills:** Comprehensive coverage of common workflows
- **Script-based skills:** Skills can include executable scripts
- **Policy-as-Code:** TOML-based policy files with HMAC integrity

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| 11 built-in skills | 6 skills implemented | **Medium** - add more skills |
| `async-pr-review` | Not implemented | **Medium** |
| `docs-writer` / `docs-changelog` | Not implemented | **Medium** |
| `code-reviewer` (structured) | Partial (`review` skill) | Low |
| `ci` skill | Not implemented | **Medium** |

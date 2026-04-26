# Implementation Plan: Skills Enhancements

> Based on analysis of 9 prior-art projects. Prioritized by impact and feasibility.
> Generated 2026-04-11.

---

## Phase 1: Expanded Skills Library (P1-P2)

### 1.1 New Built-in Skills
**Files:** `internal/tools/skills/`

- [ ] **github** - PR creation, issue management, CI status checks
- [ ] **gitlab** - GitLab equivalents
- [ ] **test-runner** - Run tests, analyze failures, suggest fixes
- [ ] **security-review** - Security-focused code analysis
- [ ] **docs-writer** - Documentation generation
- [ ] **changelog** - Changelog generation from git history
- [ ] **debug** - Bug hunting and diagnosis workflow
- [ ] **onboarding** - Repository onboarding assistance
- [ ] **refactor** - Guided refactoring workflows

---

## Phase 2: Skill Gating (P2)

### 2.1 Conditional Skill Loading
**Effort:** Low | **Reference:** OpenClaw, Gemini CLI
**Files:** `internal/tools/skill.go`

- [ ] Add optional `requires` to SKILL.md frontmatter:
  - `requires.bins`: Required binaries on PATH
  - `requires.env`: Required environment variables
  - `requires.os`: Platform filter (darwin/linux/windows)
- [ ] Skip skills that don't meet requirements during discovery
- [ ] Show gating reason in skill list

---

## Phase 3: Auto-Approval Profiles (P2-P3)

### 3.1 Predefined Profiles
**Reference:** Cline, OpenClaw

- [ ] Predefined profiles: `default` (ask), `auto-edit` (auto-approve file edits), `full` (auto-approve all)
- [ ] Configurable via settings

---

## Implementation Order (Recommended)

```
Phase 1 (New Skills) ──→ Phase 2 (Skill Gating) ──→ Phase 3 (Auto-Approval)
```

---

## Dependencies

| Item | Depends On |
|------|------------|
| Skill gating | None |
| Auto-approval profiles | None |
| Keyword-triggered skills | Skill gating (see agents/plan.md) |

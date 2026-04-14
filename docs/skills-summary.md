# Skills - Executive Summary

> Skills/commands analysis of 9 prior-art projects compared to ycode's current implementation.
> Generated 2026-04-11.

---

## Projects Surveyed

| Project | Language | Focus | Skills |
|---------|----------|-------|--------|
| [Aider](skills/aider.md) | Python | Terminal pair programming | 40+ cmds |
| [Claw Code](skills/clawcode.md) | Rust | CLI agent harness (reference) | 70+ cmds |
| [Cline](skills/cline.md) | TypeScript | VS Code extension | 6+ cmds |
| [Codex CLI](skills/codex.md) | Rust/TS | OpenAI agent | Skills crate |
| [Continue](skills/continue.md) | TypeScript | IDE extension | Rules system |
| [Gemini CLI](skills/geminicli.md) | TypeScript | Google agent | 11 built-in |
| [OpenClaw](skills/openclaw.md) | TypeScript | Multi-channel gateway | 75 bundled |
| [OpenCode](skills/opencode.md) | TypeScript | AI coding CLI | SKILL.md |
| [OpenHands](skills/openhands.md) | Python | Dev agent platform | 26 microagents |

---

## ycode Current State

**Already implemented:** 6 skills (`remember`, `loop`, `simplify`, `review`, `commit`, `pr`), SKILL.md format, skill discovery from project and user directories.

---

## Consolidated Skills Landscape

### ycode Current Skills (6)
`remember`, `loop`, `simplify`, `review`, `commit`, `pr`

### High-Value Skills from Prior Art (not yet in ycode)

| Skill | Source Projects | Description | Priority |
|-------|----------------|-------------|----------|
| **code-review** (advanced) | OpenHands, Gemini, OpenClaw | Deep code review with security focus | **P1** |
| **github/gitlab integration** | OpenHands, Gemini, OpenClaw | PR creation, issue management, CI | **P1** |
| **test-runner** | Aider, OpenHands | Run tests and fix failures | **P1** |
| **docs-writer** | Gemini CLI | Documentation generation | **P2** |
| **docs-changelog** | Gemini CLI | Changelog generation | **P2** |
| **security-review** | Claw Code, OpenHands | Security-focused code analysis | **P2** |
| **onboarding** | Continue, OpenHands | Repository onboarding assistance | **P2** |
| **refactor** | (multiple) | Guided refactoring workflows | **P2** |
| **debug** | Claw Code (bughunter) | Bug hunting and diagnosis | **P2** |
| **docker** | OpenHands | Docker/container guidance | **P3** |
| **ssh** | OpenHands | SSH operations guidance | **P3** |
| **kubernetes** | OpenHands | K8s deployment guidance | **P3** |

---

## Priority Gap Summary

### P1 - Important Gaps

1. **Expanded skill library** - GitHub/GitLab integration, test runner, advanced code review. Present across multiple projects.
2. **Skill gating** - Conditional skill loading based on available binaries, env vars, and OS (OpenClaw, Gemini CLI pattern).

### P2 - Valuable Enhancements

3. **Documentation skills** - docs-writer, docs-changelog (Gemini CLI pattern).
4. **Security review skill** - Security-focused code analysis (OpenHands, Claw Code).
5. **Onboarding skill** - Repository onboarding assistance (Continue, OpenHands).
6. **Keyword-triggered skills** - Auto-activate skills based on conversation content (OpenHands).
7. **Rules system** - Auto-attach, agent-requested rules (Continue pattern).

### P3 - Nice-to-Have

8. **Platform-specific skills** - Docker, SSH, Kubernetes guidance.
9. **Auto-approval profiles** - Presets like YOLO/coding/minimal for skill-related operations.

---

## Implementation Plan

See [skills/plan.md](skills/plan.md) for the detailed skills implementation plan.

---

## Per-Project Documentation

| Project | Document |
|---------|----------|
| Aider | [aider.md](skills/aider.md) |
| Claw Code | [clawcode.md](skills/clawcode.md) |
| Cline | [cline.md](skills/cline.md) |
| Codex CLI | [codex.md](skills/codex.md) |
| Continue | [continue.md](skills/continue.md) |
| Gemini CLI | [geminicli.md](skills/geminicli.md) |
| OpenClaw | [openclaw.md](skills/openclaw.md) |
| OpenCode | [opencode.md](skills/opencode.md) |
| OpenHands | [openhands.md](skills/openhands.md) |

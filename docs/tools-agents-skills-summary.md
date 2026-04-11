# Tools, Agents & Skills - Executive Summary

> Comprehensive analysis of 9 prior-art projects compared to ycode's current implementation.
> Generated 2026-04-11.

---

## Projects Surveyed

| Project | Language | Focus | Tools | Agents | Skills |
|---------|----------|-------|-------|--------|--------|
| [Aider](tools-agents-skills/aider.md) | Python | Terminal pair programming | 2 | 11 modes | 40+ cmds |
| [Claw Code](tools-agents-skills/clawcode.md) | Rust | CLI agent harness (reference) | 50 | Workers/Teams | 70+ cmds |
| [Cline](tools-agents-skills/cline.md) | TypeScript | VS Code extension | 24 | Parallel subagents | 6+ cmds |
| [Codex CLI](tools-agents-skills/codex.md) | Rust/TS | OpenAI agent | 23 | V2 task trees | Skills crate |
| [Continue](tools-agents-skills/continue.md) | TypeScript | IDE extension | 20 | Model-capability | Rules system |
| [Gemini CLI](tools-agents-skills/geminicli.md) | TypeScript | Google agent | 20 | 5 local + A2A | 11 built-in |
| [OpenClaw](tools-agents-skills/openclaw.md) | TypeScript | Multi-channel gateway | 21+ | ACP + sub-agents | 75 bundled |
| [OpenCode](tools-agents-skills/opencode.md) | TypeScript | AI coding CLI | 19 | 4 agents | SKILL.md |
| [OpenHands](tools-agents-skills/openhands.md) | Python | Dev agent platform | 15 | 6 agents | 26 microagents |

---

## ycode Current State

**Already implemented:** 50+ tools, 6 agent types, 6 skills, 3-tier permissions (ReadOnly/WorkspaceWrite/DangerFullAccess), VFS path validation, MCP/LSP integration, Worker/Team/Cron systems, session compaction, hook system.

See detailed per-project docs in `docs/tools-agents-skills/` for complete inventories.

---

## Consolidated Tool Landscape

### Universal Tools (present in all/most projects)
All projects implement these core categories. **ycode has all of these.**

| Category | Tools | ycode Status |
|----------|-------|-------------|
| File read/write/edit | read, write, edit, patch | **Done** |
| Search (glob/grep) | Pattern and content search | **Done** |
| Shell execution | Bash/shell commands | **Done** |
| Web fetch/search | URL fetch + web search | **Done** |
| User interaction | Ask questions, confirmations | **Done** |
| Task management | Todo/task lists | **Done** |
| Plan mode | Enter/exit planning | **Done** |
| Agent spawning | Subagent delegation | **Done** |
| MCP integration | External tool servers | **Done** |
| LSP integration | Code intelligence | **Done** |
| Skill loading | Reusable prompt definitions | **Done** |
| Tool search | Deferred tool discovery | **Done** |

### Differentiated Tools (present in some projects)

| Tool | Projects | ycode Status | Priority |
|------|----------|-------------|----------|
| **Browser automation** | Cline, Gemini, OpenClaw, OpenHands | Not implemented | **P0 - High** |
| **Image viewing/analysis** | Codex, OpenClaw | Not implemented | **P1 - Medium** |
| **apply_patch (multi-file atomic)** | Codex, Cline, OpenCode, OpenClaw | Not implemented | **P1 - Medium** |
| **REPL (Python/JS)** | Codex (JS), OpenHands (IPython) | Not implemented | **P2 - Medium** |
| **view_diff (git diff tool)** | Continue, Aider | Not implemented | **P2 - Medium** |
| **Inter-agent messaging** | Codex (V2), OpenClaw | Not implemented | **P2 - Medium** |
| **Batch read (multiple files)** | Gemini CLI, ycode | **Done** (read_multiple_files) |
| **Media generation** | OpenClaw (image/video/music/TTS) | N/A | Low - not core |
| **Device control** | OpenClaw (nodes, camera, location) | N/A | Low - not core |

---

## Consolidated Agent Landscape

### Agent Patterns Across Projects

| Pattern | Projects | Description | ycode Status |
|---------|----------|-------------|-------------|
| **Explore/ReadOnly agent** | ycode, OpenCode, OpenHands, Gemini | Read-only codebase exploration | **Done** |
| **Plan agent** | ycode, OpenCode, Cline, Gemini | Read-only planning mode | **Done** |
| **General-purpose agent** | ycode, OpenCode, Gemini | Full-featured delegation | **Done** |
| **Codebase investigator** | Gemini CLI | Deep code analysis, JSON reports | Partial (Explore) |
| **Browser agent** | Gemini CLI, OpenHands | Autonomous web browsing | **Not implemented** |
| **Memory manager agent** | Gemini CLI | Memory persistence management | Partial (memory system) |
| **Architect → Editor delegation** | Aider | Plan then implement with different models | **Not implemented** |
| **V2 task trees + mailbox** | Codex | Hierarchical agent communication | **Not implemented** |
| **ACP/A2A remote agents** | OpenClaw, Gemini CLI | Inter-harness agent protocol | **Not implemented** |
| **Custom agents (user-defined)** | OpenCode, OpenClaw | Config-based agent creation | **Not implemented** |
| **Keyword-triggered agents** | OpenHands | Auto-activate on conversation content | **Not implemented** |

---

## Consolidated Security Landscape

### Permission/Approval Systems

| Feature | Projects | ycode Status | Priority |
|---------|----------|-------------|----------|
| 3-tier permission modes | ycode, Claw Code, Codex | **Done** | - |
| Policy rules (allow/deny/ask) | ycode, Claw Code, Gemini, OpenCode | **Done** | - |
| Hook system (pre/post tool) | ycode, Claw Code, Codex, Gemini | **Done** | - |
| **Guardian LLM review** | Codex | Not implemented | **P0 - High** |
| **Platform sandboxing** | Codex (Seatbelt/Landlock/bwrap), Gemini (sandbox-exec/Docker) | Not implemented | **P0 - High** |
| **Docker runtime sandboxing** | OpenHands, OpenClaw | Not implemented | **P0 - High** |
| **Bash AST analysis** | OpenCode (tree-sitter) | Not implemented | **P0 - High** |
| **Bash command classification** | Claw Code (intent: Read/Write/Destructive) | Basic | **P1 - Medium** |
| **Network approval system** | Codex (host allowlist, protocol control) | Not implemented | **P1 - Medium** |
| **Approval caching** | Codex, Cline | Not implemented | **P1 - Medium** |
| **SSRF protection** | OpenClaw (DNS pinning, IP blocking) | Not implemented | **P1 - Medium** |
| **.env file protection** | OpenCode (ask before reading .env) | Not implemented | **P1 - Medium** |
| **Ignore files (.ycodeignore)** | Cline (.clineignore), Gemini (.geminiignore) | Not implemented | **P1 - Medium** |
| **Shell redirect blocking** | Cline (blocks >, >>, <, >&) | Not implemented | **P1 - Medium** |
| **Security analyzer framework** | OpenHands (pluggable: LLM, Invariant, GraySwan) | Not implemented | **P2 - Medium** |
| **Folder trust system** | Gemini CLI | Not implemented | **P2 - Medium** |
| **Per-tool risk levels** | OpenHands (LOW/MEDIUM/HIGH) | Not implemented | **P2 - Medium** |
| **Doom loop detection** | OpenCode, OpenHands ("almost stuck") | Not implemented | **P2 - Medium** |
| **Auto-approval profiles** | Cline (YOLO/per-action), OpenClaw (tool profiles) | Not implemented | **P2 - Medium** |
| **Binary file detection** | Claw Code (NUL byte check) | Not implemented | **P2 - Medium** |
| **Content filtering** | OpenClaw (strip thinking tags, control tokens) | Not implemented | **P2 - Medium** |

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

### P0 - Critical Gaps (High impact, widely implemented elsewhere)

1. **Platform sandboxing** - Seatbelt (macOS), Landlock/bwrap (Linux). Present in Codex, Gemini CLI, OpenHands, OpenClaw. Essential for safe autonomous execution.

2. **Bash command safety analysis** - Tree-sitter AST parsing (OpenCode) or intent classification (Claw Code). Current ycode has basic validation only.

3. **Browser automation tool** - Puppeteer/Chrome control. Present in Cline, Gemini CLI, OpenClaw, OpenHands. Enables web research and testing.

4. **Guardian/LLM-based approval review** - Codex's dedicated LLM reviewer for tool approvals. Novel approach to security that complements static rules.

### P1 - Important Gaps (Medium-high impact)

5. **apply_patch tool** - Multi-file atomic patch application. Present in Codex, Cline, OpenCode, OpenClaw.

6. **Network security** - SSRF protection, network approval system, shell redirect blocking.

7. **Sensitive file protection** - .env file guards, .ycodeignore support, binary file detection.

8. **Approval caching** - Remember user decisions within session to avoid repeated prompts.

9. **Expanded skill library** - GitHub/GitLab integration, test runner, security review, docs generation.

10. **Image viewing tool** - View screenshots and images for visual context.

### P2 - Valuable Enhancements (Medium impact)

11. **Custom agent definitions** - User-defined agents via config files (OpenCode, OpenClaw pattern).
12. **Inter-agent messaging** - Direct communication between spawned agents.
13. **Keyword-triggered skills** - Auto-activate skills based on conversation content.
14. **Doom loop detection** - Detect and recover when agent is stuck.
15. **Security analyzer framework** - Pluggable security analysis (OpenHands pattern).
16. **Auto-approval profiles** - Presets like YOLO/coding/minimal (OpenClaw pattern).
17. **Git diff tool** - Expose git diff as an LLM-callable tool.
18. **REPL tool** - Python/JS REPL for data exploration.
19. **Repository mapping** - AST-based codebase overview (Aider pattern).

---

## Implementation Plan

See [docs/tools-agents-skills/plan.md](tools-agents-skills/plan.md) for the detailed implementation plan and todo list.

---

## Per-Project Documentation

| Project | Document |
|---------|----------|
| Aider | [aider.md](tools-agents-skills/aider.md) |
| Claw Code | [clawcode.md](tools-agents-skills/clawcode.md) |
| Cline | [cline.md](tools-agents-skills/cline.md) |
| Codex CLI | [codex.md](tools-agents-skills/codex.md) |
| Continue | [continue.md](tools-agents-skills/continue.md) |
| Gemini CLI | [geminicli.md](tools-agents-skills/geminicli.md) |
| OpenClaw | [openclaw.md](tools-agents-skills/openclaw.md) |
| OpenCode | [opencode.md](tools-agents-skills/opencode.md) |
| OpenHands | [openhands.md](tools-agents-skills/openhands.md) |

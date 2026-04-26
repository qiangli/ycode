# Tools - Executive Summary

> Tool analysis of 9 prior-art projects compared to ycode's current implementation.
> Generated 2026-04-11.

---

## Projects Surveyed

| Project | Language | Focus | Tools |
|---------|----------|-------|-------|
| [Aider](research/tools/aider.md) | Python | Terminal pair programming | 2 |
| [Claw Code](research/tools/clawcode.md) | Rust | CLI agent harness (reference) | 50 |
| [Cline](research/tools/cline.md) | TypeScript | VS Code extension | 24 |
| [Codex CLI](research/tools/codex.md) | Rust/TS | OpenAI agent | 23 |
| [Continue](research/tools/continue.md) | TypeScript | IDE extension | 20 |
| [Gemini CLI](research/tools/geminicli.md) | TypeScript | Google agent | 20 |
| [OpenClaw](research/tools/openclaw.md) | TypeScript | Multi-channel gateway | 21+ |
| [OpenCode](research/tools/opencode.md) | TypeScript | AI coding CLI | 19 |
| [OpenHands](research/tools/openhands.md) | Python | Dev agent platform | 15 |

---

## ycode Current State

**Already implemented:** 50+ tools covering file ops, search, shell, web, user interaction, task management, plan mode, agent spawning, MCP/LSP integration, tool search.

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

9. **Image viewing tool** - View screenshots and images for visual context.

### P2 - Valuable Enhancements (Medium impact)

10. **Doom loop detection** - Detect and recover when agent is stuck.
11. **Security analyzer framework** - Pluggable security analysis (OpenHands pattern).
12. **Auto-approval profiles** - Presets like YOLO/coding/minimal (OpenClaw pattern).
13. **Git diff tool** - Expose git diff as an LLM-callable tool.
14. **REPL tool** - Python/JS REPL for data exploration.
15. **Repository mapping** - AST-based codebase overview (Aider pattern).

---

## Implementation Plan

See [research/tools/plan.md](research/tools/plan.md) for the detailed tools implementation plan.

---

## Per-Project Documentation

| Project | Document |
|---------|----------|
| Aider | [aider.md](research/tools/aider.md) |
| Claw Code | [clawcode.md](research/tools/clawcode.md) |
| Cline | [cline.md](research/tools/cline.md) |
| Codex CLI | [codex.md](research/tools/codex.md) |
| Continue | [continue.md](research/tools/continue.md) |
| Gemini CLI | [geminicli.md](research/tools/geminicli.md) |
| OpenClaw | [openclaw.md](research/tools/openclaw.md) |
| OpenCode | [opencode.md](research/tools/opencode.md) |
| OpenHands | [openhands.md](research/tools/openhands.md) |

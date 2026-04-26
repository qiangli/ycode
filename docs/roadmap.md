# Roadmap

> Consolidated view of features not yet implemented, extracted from gap analyses and prior-art research. See the source documents for detailed rationale.

---

## P0 — Critical

| Feature | Source | Description |
|---------|--------|-------------|
| **Browser automation tool** | [agents-summary](agents-summary.md), [tools-summary](tools-summary.md) | Autonomous web browsing via Chrome/Puppeteer. Present in Gemini CLI, OpenHands, Cline, OpenClaw. |
| **Platform sandboxing** | [tools-summary](tools-summary.md) | Seatbelt (macOS), Landlock/bwrap (Linux). Present in Codex, Gemini CLI, OpenHands. Essential for safe autonomous execution. |
| **Bash AST safety analysis** | [tools-summary](tools-summary.md) | Tree-sitter AST parsing (OpenCode) or intent classification. Current ycode has basic validation only. |
| **Guardian/LLM approval review** | [tools-summary](tools-summary.md) | Dedicated LLM reviewer for tool approvals (Codex pattern). Complements static permission rules. |
| **Sensitive data redaction in OTEL** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Pattern-based redaction for API keys, tokens, PII in trace/log attributes. |
| **Evaluation framework** | [evaluation](evaluation.md) | 4-tier eval pyramid (contract/smoke/behavioral/E2E), pass@k scoring, regression detection, self-contained scheduling. Foundation for self-improving agent loop. |

---

## P1 — Important

| Feature | Source | Description |
|---------|--------|-------------|
| **Custom agent definitions** | [agents-summary](agents-summary.md) | User-defined agents via config files (OpenCode, OpenClaw pattern). |
| **A2A/ACP remote agents** | [agents-summary](agents-summary.md) | Inter-harness agent protocol for cross-tool interop. |
| **apply_patch tool** | [tools-summary](tools-summary.md) | Multi-file atomic patch application. Present in Codex, Cline, OpenCode, OpenClaw. |
| **Image viewing tool** | [tools-summary](tools-summary.md) | View screenshots and images for visual context. |
| **Model fallback chains** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Ordered fallback across providers/models on failure (429, 5xx, timeout). |
| **Session lifecycle state machine** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Track idle → processing → waiting with diagnostic events. |
| **Subagent session isolation** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Dedicated JSONL for subagent transcripts, linked to parent session. |
| **Tool allowlisting per agent/context** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Per-plugin and per-session tool allowlists beyond permission modes. |
| **Structured diagnostic events** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Taxonomy of model.usage, session.state, tool.loop events on bus. |
| **Diagnostic event → OTEL span mapping** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Map structured diagnostic events to trace spans with attributes. |
| **Network security** | [tools-summary](tools-summary.md) | SSRF protection, host allowlists, shell redirect blocking. |
| **Sensitive file protection** | [tools-summary](tools-summary.md) | .env file guards, .ycodeignore support, binary file detection. |
| **Expanded skill library** | [skills-summary](skills-summary.md) | GitHub/GitLab integration, test runner, advanced code review. |
| **Skill gating** | [skills-summary](skills-summary.md) | Conditional skill loading based on available binaries, env vars, OS. |
| **Google Gemini native provider** | [gap-analysis-opencode](gap-analysis-opencode.md) | Growing model family with unique features (grounding, long context). |
| **Session retry/revert** | [gap-analysis-opencode](gap-analysis-opencode.md) | Undo last assistant turn, re-run with different params. |
| **TUI status bar** | [gap-analysis-opencode](gap-analysis-opencode.md) | Model, tokens, cost, session info always visible. |
| **TUI model picker** | [gap-analysis-opencode](gap-analysis-opencode.md) | Switch models mid-conversation via dialog. |

---

## P2 — Valuable Enhancements

| Feature | Source | Description |
|---------|--------|-------------|
| **V2 task trees + mailbox** | [agents-summary](agents-summary.md) | Hierarchical agent communication (Codex pattern). |
| **Keyword-triggered agents** | [agents-summary](agents-summary.md) | Auto-activate based on conversation content (OpenHands pattern). |
| **Architect → Editor delegation** | [agents-summary](agents-summary.md) | Plan with one model, implement with another (Aider pattern). |
| **Doom loop detection** | [tools-summary](tools-summary.md) | Detect and recover when agent is stuck (OpenCode, OpenHands). |
| **Security analyzer framework** | [tools-summary](tools-summary.md) | Pluggable security analysis (OpenHands pattern). |
| **Auto-approval profiles** | [tools-summary](tools-summary.md) | Presets like YOLO/coding/minimal (OpenClaw pattern). |
| **REPL tool** | [tools-summary](tools-summary.md) | Python/JS REPL for data exploration. |
| **Repository mapping** | [tools-summary](tools-summary.md) | AST-based codebase overview (Aider pattern). |
| **Documentation skills** | [skills-summary](skills-summary.md) | docs-writer, docs-changelog (Gemini CLI pattern). |
| **Security review skill** | [skills-summary](skills-summary.md) | Security-focused code analysis (OpenHands, Claw Code). |
| **Tool loop detection events** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Structured events for repeat/ping-pong/circuit-breaker patterns. |
| **Stuck session detection** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Background goroutine checking session age and queue depth. |
| **Outbound delivery queue** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | File-based persistent queue with crash recovery and retry. |
| **Per-session model overrides** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Switch model mid-session via service API. |
| **Auth profile rotation** | [gap-analysis-openclaw](gap-analysis-openclaw.md) | Multiple API keys per provider, rotate on rate limits. |
| **TUI command palette** | [gap-analysis-opencode](gap-analysis-opencode.md) | Fuzzy-searchable list of all commands/actions. |

---

## Decided Against / Deferred Indefinitely

| Feature | Reason |
|---------|--------|
| Azure OpenAI native provider | OpenAI-compatible adapter covers it |
| Config JSONC / schema validation | Current JSON 3-tier merge is sufficient |
| Enterprise/MDM config deployment | Out of scope for current project |
| Electron desktop app | CLI-first; web UI covers GUI needs |
| MCP SSE/HTTP transport | Stdio transport covers primary use cases |
| File watcher | Nice-to-have for long sessions, low priority |
| Input provenance tracking | Low impact |
| Memory retention policies | Memory files are small; manual cleanup sufficient |

---

## Source Documents

- [agents-summary.md](agents-summary.md) — agent architecture gaps
- [skills-summary.md](skills-summary.md) — skills landscape gaps
- [tools-summary.md](tools-summary.md) — tools and security gaps
- [gap-analysis-memos.md](gap-analysis-memos.md) — Memos integration gaps (mostly resolved)
- [gap-analysis-openclaw.md](gap-analysis-openclaw.md) — OpenClaw feature gaps
- [gap-analysis-opencode.md](gap-analysis-opencode.md) — OpenCode feature gaps

# Gap Analysis: ycode vs OpenCode

## Overview

This document compares ycode (pure Go CLI agent harness) with OpenCode (TypeScript/Bun-based AI coding assistant) to identify feature gaps worth closing. OpenCode uses Effect + Vercel AI SDK + Solid.js; ycode uses Go stdlib + Cobra + Bubbletea.

---

## Feature Comparison Matrix

| Feature Area | ycode | OpenCode | Gap? |
|---|---|---|---|
| **LLM Providers** | Anthropic + OpenAI-compatible (covers ~5 providers) | 20+ native providers (Google, Azure, Bedrock, Mistral, Groq, Cohere, XAI, etc.) | YES |
| **TUI** | Bubbletea (basic REPL, markdown rendering) | OpenTUI/Solid.js (dialogs, themes, command palette, session list, model picker) | YES |
| **Session Storage** | JSONL files with rotation | SQLite + Drizzle ORM with migrations | YES |
| **Session Features** | Basic persist/resume/compact | Versioning, retry/revert, sharing, slug-based addressing | YES |
| **Git Integration** | Basic context (branch, diff) | Full git ops + GitHub API via Octokit (PR create/update, issues, repos) | YES |
| **MCP** | Client + server + stdio transport + tool bridge | Client (stdio/SSE/HTTP) + OAuth + resource/prompt management | PARTIAL |
| **LSP** | Client + hover/definition/references/symbols | Same + per-language server configs (TS, Python, Go, Bash) | PARTIAL |
| **File Operations** | VFS + read/write/edit/glob/grep + boundary enforcement | Similar + ripgrep binary + file watcher | PARTIAL |
| **Permissions** | 4 modes (read-only → danger-full-access) + per-tool | Rule-based with wildcards + per-session approvals + feedback | PARTIAL |
| **Configuration** | 3-tier JSON merge (user/project/local) | JSONC + 5-tier merge + MDM/enterprise deployment | YES |
| **Plugin System** | Basic manager + hooks (pre/post tool use) | npm-based + TUI extensions + built-in auth plugins | PARTIAL |
| **Desktop App** | None | Electron app | NO (out of scope) |
| **Web Console** | Basic WebSocket dashboard | Solid.js Start full web app | PARTIAL |
| **Observability** | Full embedded stack (Prometheus, Jaeger, VictoriaLogs, Perses, Alertmanager) | None | ycode ahead |
| **Memory System** | 5-layer (working → persistent) + vector search + auto-dream | None built-in | ycode ahead |
| **Worker/Team** | Worker lifecycle + team management + NATS | None | ycode ahead |
| **Self-Healing** | Panic recovery + AI diagnosis + auto-fix | None | ycode ahead |
| **Embedding API** | `pkg/ycode/` with functional options | None (CLI-only) | ycode ahead |
| **Scheduling** | Loop controller + cron via NATS | None | ycode ahead |

---

## Identified Gaps (Priority Order)

### Gap 1: LLM Provider Coverage

**Current**: Anthropic native + OpenAI-compatible adapter (covers OpenAI, xAI, Ollama, OpenRouter, DashScope).

**Missing**:
- **Google Gemini** (Generative AI + Vertex AI) — growing model family, unique features (grounding, long context)
- **Azure OpenAI** — enterprise customers often mandate Azure
- **Amazon Bedrock** — same for AWS shops
- **Mistral** — strong open-weight models, different API shape
- **Groq** — ultra-fast inference, different rate limits
- **Cohere** — enterprise search/RAG, Command models
- **Cloudflare Workers AI** — edge inference

**Recommendation**: Add Google Gemini as a native provider. Azure OpenAI deferred — OpenAI-compatible adapter covers it adequately. Others deferred since most work through OpenAI-compatible adapter already.

### Gap 2: TUI Sophistication

**Current**: Single-pane REPL with markdown rendering, basic slash command completion.

**Missing**:
- **Command palette** — fuzzy-searchable list of all commands/actions
- **Model/provider picker dialog** — switch models mid-conversation
- **Session list dialog** — browse/switch/rename sessions visually
- **Theme system** — light/dark themes, customizable colors
- **Split pane views** — side-by-side diff, file preview
- **Status bar** — model, tokens, cost, session info always visible
- **Toast notifications** — non-blocking feedback messages
- **MCP status dialog** — view connected MCP servers and their status

**Recommendation**: Incremental TUI enhancement. Start with status bar and model picker, then command palette. Session list dialog deferred — session detail/search will integrate with the existing OTEL stack (VictoriaLogs, Jaeger, Perses) instead of a TUI-native browser.

### Gap 3: Session Management Improvements

**Current**: JSONL files, auto-compaction at 100K tokens, basic resume.

**Missing**:
- **Session retry/revert** — undo last assistant turn, re-run with different params
- **Session sharing** — export shareable session URL/file
- **Message versioning** — track message edits/regenerations
- **Session slug** — human-readable identifiers (not just UUIDs)
- **Session summary stats** — files changed, lines added/deleted per session
- **Session title generation** — auto-generate from first user message

**Recommendation**: Add retry/revert first (highest user value), then title generation. Summary stats deferred — session metrics will be emitted to the OTEL stack (Prometheus, VictoriaLogs) rather than tracked in-app.

### Gap 4: Git & GitHub Integration Depth

**Current**: Branch/diff context for prompts, `view_diff` tool. GitHub interaction via external `gh` CLI.

**Missing**:
- **Native GitHub API** — PR creation/update, issue management, review comments without `gh` CLI dependency
- **Git operations as tools** — commit, branch, stash, cherry-pick as first-class tools
- **Merge base detection** — for accurate PR diff calculation
- **Git statistics** — line counts, file change summaries
- **Repository cloning** — clone repos for analysis

**Recommendation**: Git operations as tools is highest value. Native GitHub API is nice-to-have since `gh` CLI works well.

### Gap 5: Configuration Enhancements

**Current**: 3-tier JSON merge (user → project → local).

**Missing**:
- **JSONC support** — comments in config files (developer-friendly)
- **Config validation** — schema-based validation with detailed error messages
- **Config watching** — live reload on file changes
- **Enterprise/MDM support** — managed configuration deployment (low priority for POC)

**Recommendation**: Won't do. Current JSON config with 3-tier merge is sufficient for POC scope.

### Gap 6: Permission System Refinement

**Current**: 4 permission modes + per-tool level declarations.

**Missing**:
- **Pattern-based rules** — wildcard matching for fine-grained control (e.g., allow `bash:git *` but deny `bash:rm -rf *`)
- **Per-session approval tracking** — "always allow" within a session, not globally
- **Permission feedback** — user can provide correction context when denying
- **Rule persistence** — save learned rules across sessions

**Recommendation**: Deferred. Current permission system is functional for POC scope.

### Gap 7: MCP Enhancements

**Current**: Client + server + stdio transport + tool bridge.

**Missing**:
- **SSE transport** — HTTP-based MCP server connections
- **Streamable HTTP transport** — newer MCP transport
- **OAuth flow for MCP** — authenticate with MCP servers requiring OAuth
- **MCP resource/prompt support** — beyond just tools
- **MCP server status tracking** — connected/failed/needs-auth states
- **MCP management UI** — TUI dialog for server status and management

**Recommendation**: Deferred. Current stdio transport + tool bridge covers primary use cases.

### Gap 8: File Watcher

**Current**: No file watching (except loop mode watching prompt files).

**Missing**:
- **Workspace file watcher** — detect external file changes during conversation
- **Auto-refresh context** — re-read modified files before tool use
- **Change notification** — alert when files changed outside ycode

**Recommendation**: Low priority. Nice-to-have for long-running sessions.

---

## Out of Scope

These OpenCode features are intentionally excluded:

- **Electron desktop app** — ycode is CLI-first, web UI covers GUI needs
- **Effect-based architecture** — Go's stdlib patterns are sufficient
- **npm plugin ecosystem** — Go plugin system with different approach
- **Marketing website** — not relevant to agent functionality
- **Bun runtime** — Go single binary is a strength

# Implementation Plan: Closing OpenCode Gaps

## Phased Approach

Work is organized into 3 phases. Each phase builds on the previous one. Within each phase, items can be worked in parallel.

**Scoping decisions:**
- Only Google Gemini provider (Azure deferred — OpenAI-compat covers it adequately)
- JSONC config and config validation: won't do (current JSON config is sufficient)
- Session list dialog: deferred — session detail/search will integrate with the OTEL stack instead
- Session summary stats: deferred — metrics will integrate with the OTEL stack instead
- Phase 4 (MCP & Permissions): deferred entirely

---

## Phase 1: Provider Foundation

### 1.1 Google Gemini Provider
- Add `internal/api/google.go` implementing the Provider interface
- Support Generative AI API (generativelanguage.googleapis.com)
- Handle Gemini-specific features: grounding, safety settings, long context
- Add model aliases: `gemini-pro`, `gemini-flash`, `gemini-ultra`
- Auth: `GOOGLE_API_KEY` or `GOOGLE_APPLICATION_CREDENTIALS`
- Add streaming via SSE
- Unit tests with mock server

---

## Phase 2: TUI Enhancement (User Experience)

### 2.1 Status Bar
- Add persistent bottom bar to Bubbletea model
- Display: current model, token count, estimated cost, session ID, permission mode
- Update in real-time during streaming
- Toggle visibility with keybinding

### 2.2 Model/Provider Picker
- Keybinding (e.g., `Ctrl+M`) opens model selection overlay
- List available models grouped by provider
- Show model capabilities (vision, thinking, max tokens)
- Switch model mid-conversation without restart
- Persist last-used model preference

### 2.3 Command Palette
- Keybinding (e.g., `Ctrl+P` or `Ctrl+K`) opens fuzzy search overlay
- Index all slash commands, keybindings, and common actions
- Fuzzy matching with scoring (consider `github.com/sahilm/fuzzy`)
- Execute selected command inline
- Show keybinding hints next to commands

### 2.4 Session Detail & Search via OTEL
- Leverage existing OTEL stack (VictoriaLogs, Jaeger, Prometheus) for session visibility
- Emit structured session logs (start, end, title, token usage, files changed) to VictoriaLogs
- Session traces in Jaeger — each session as a trace, each turn as a span
- Session metrics in Prometheus — token counts, tool usage, cost per session
- Perses dashboard for session overview (active sessions, historical search, cost trends)
- Query sessions via LogsQL in VictoriaLogs instead of building a TUI session browser
- Add `/sessions` command that opens the Perses dashboard or queries VictoriaLogs

### 2.5 Toast Notifications
- Non-blocking notification system in TUI
- Auto-dismiss after timeout
- Stack multiple notifications
- Severity levels: info, warning, error, success
- Use for: tool completion, permission requests, background task updates

---

## Phase 3: Session & Git (Core Workflow)

### 3.1 Session Retry/Revert
- Add `/retry` command — remove last assistant turn, re-send with same or modified prompt
- Add `/revert` command — undo file changes made in last assistant turn
- Track file modifications per turn (before/after snapshots or git stash)
- Store revert metadata in session JSONL
- Handle edge cases: multiple tool calls per turn, partial reverts

### 3.2 Session Title Generation
- Auto-generate title from first user message using LLM (short, 5-10 words)
- Fallback: first 50 chars of first message
- Allow manual rename via `/rename <title>`
- Store title in session metadata
- Display in status bar and OTEL session logs

### 3.3 Session & Turn Metrics via OTEL
- Emit per-turn metrics to Prometheus: files changed, lines added/deleted, tools invoked
- Emit per-session summary as structured log to VictoriaLogs on session close
- Add session-level attributes to existing OTEL traces (file count, line delta)
- Build Perses dashboard panel for per-session activity breakdown
- No separate in-app summary stats — OTEL is the source of truth

### 3.4 Git Operations as Tools
- Add `git_commit` tool — stage + commit with message
- Add `git_branch` tool — create/switch/delete branches
- Add `git_stash` tool — stash/pop/list
- Add `git_log` tool — structured log output
- Add `git_status` tool — structured status (not just context)
- Each tool respects permission system (workspace-write or higher)
- Add to tool registry with proper specs

### 3.5 Merge Base Detection
- Add `git merge-base` wrapper to `runtime/git/`
- Use for accurate PR diff calculation
- Expose via `view_diff` tool enhancement
- Calculate accurate file change stats for PRs

---

## Deferred (Punted)

| Item | Reason |
|------|--------|
| 1.2 Azure OpenAI | OpenAI-compat adapter covers it adequately |
| 1.3 JSONC Config | Current JSON config is sufficient |
| 1.4 Config Validation | Current config handling is sufficient |
| 2.4 Session List Dialog (TUI) | Replaced by OTEL-integrated session search (2.4 revised) |
| 3.3 Session Summary Stats (in-app) | Replaced by OTEL metrics (3.3 revised) |
| Phase 4 (MCP SSE/HTTP, OAuth, Pattern Permissions) | Entire phase deferred |

---

## Dependency Graph

```
Phase 1 (provider)
  └── 1.1 Google Gemini ─────────────────────┐
                                              │
Phase 2 (TUI) ◄──────────────────────────────┘
  ├── 2.1 Status Bar ────────────────────────┐
  ├── 2.2 Model Picker ──► needs 2.1        │
  ├── 2.3 Command Palette ──────────────────┤ 2.1 first, rest parallel
  ├── 2.4 Session Detail/Search (OTEL) ─────┤
  └── 2.5 Toast Notifications ──────────────┘
                                              │
Phase 3 (session/git) ◄──────────────────────┘
  ├── 3.1 Retry/Revert ─────────────────────┐
  ├── 3.2 Title Generation ──────────────────┤
  ├── 3.3 Session Metrics (OTEL) ───────────┤ all independent
  ├── 3.4 Git Tools ─────────────────────────┤
  └── 3.5 Merge Base ───► needs 3.4         ┘
```

## Estimated Effort (Relative)

| Item | Size | Notes |
|------|------|-------|
| 1.1 Google Gemini | M | Different API shape from OpenAI |
| 2.1 Status Bar | S | Bubbletea component |
| 2.2 Model Picker | M | Overlay + provider enumeration |
| 2.3 Command Palette | M | Fuzzy search + overlay |
| 2.4 Session Detail/Search (OTEL) | M | Structured logs + Perses dashboard |
| 2.5 Toast Notifications | S | Timer-based overlay |
| 3.1 Retry/Revert | L | File snapshot tracking |
| 3.2 Title Generation | S | LLM call + metadata |
| 3.3 Session Metrics (OTEL) | S | Metrics emission + dashboard |
| 3.4 Git Tools | M | 5 tools + specs |
| 3.5 Merge Base | S | Git wrapper |

S = small (< 1 day), M = medium (1-3 days), L = large (3-5 days)

# TODO: OpenCode Gap Implementation Tracking

## Phase 1: Provider Foundation

- [x] **1.1 Google Gemini Provider** (M) — `2f20aa3`
  - [x] Route through OpenAI-compatible API at generativelanguage.googleapis.com
  - [x] Model aliases (gemini-pro, gemini-flash)
  - [x] Auth (GOOGLE_API_KEY, GEMINI_API_KEY)
  - [x] 1M token context window capability
  - [x] Provider detection from model name prefix
  - [x] Unit tests

## Phase 2: TUI Enhancement

- [x] **2.1 Status Bar** (S) — `8d084b3`
  - [x] Enhanced bottom bar with token count, cost, session ID/title
  - [x] Real-time update during streaming
  - [x] Updated hint text with ctrl+k reference

- [x] **2.2 Model/Provider Picker** (M) — `8d084b3`
  - [x] Overlay dialog with fuzzy filter
  - [x] List models grouped by provider
  - [x] Mid-conversation model switch
  - [x] Keyboard navigation (↑/↓, Enter, Esc)

- [x] **2.3 Command Palette** (M) — `8d084b3`
  - [x] Fuzzy search overlay (Ctrl+K)
  - [x] Index slash commands + built-in actions
  - [x] Fuzzy matching algorithm
  - [x] Keybinding hints
  - [x] Unit tests

- [x] **2.4 Session Detail & Search via OTEL** (M) — `ea36c26`
  - [x] Session summary logging to VictoriaLogs (SessionSummary type)
  - [x] Session-level metrics (duration, cost, tokens) in Prometheus
  - [x] Turn-level file change metrics (files changed, lines added/deleted)
  - [x] Updated Perses dashboards with session panels

- [x] **2.5 Toast Notifications** (S) — `8d084b3`
  - [x] Non-blocking notification component with auto-dismiss
  - [x] Stacking support (max 3 visible)
  - [x] Severity levels (info, success, warning, error) with icons
  - [x] Unit tests

## Phase 3: Session & Git

- [x] **3.1 Session Retry/Revert** (L) — `61169fa`
  - [x] `/retry` command (remove last turn, re-send)
  - [x] `/revert` command (undo uncommitted file changes via git checkout)
  - [x] RemoveLastTurn correctly handles tool use exchanges
  - [x] LastUserMessage retrieval
  - [x] Unit tests

- [x] **3.2 Session Title Generation** (S) — `7cdf9a5`
  - [x] Auto-generate title from first user message
  - [x] Truncation at 50 chars, strip newlines
  - [x] `/rename <title>` command
  - [x] Display in status bar (title or session ID fallback)
  - [x] Unit tests

- [x] **3.3 Session & Turn Metrics via OTEL** (S) — `ea36c26`
  - [x] Session duration, cost, token counters in Prometheus
  - [x] Turn-level files changed, lines added/deleted histograms
  - [x] SessionSummary structured log to VictoriaLogs
  - [x] Updated Perses dashboard panels

- [x] **3.4 Git Operations as Tools** (M) — `8dacd01`
  - [x] `git_commit` tool (stage + commit)
  - [x] `git_branch` tool (list/create/switch/delete)
  - [x] `git_stash` tool (push/pop/list/drop/show)
  - [x] `git_log` tool (structured output with filters)
  - [x] `git_status` tool (short format)
  - [x] Permission integration (ReadOnly for status/log, WorkspaceWrite for commit/branch/stash)
  - [x] Tool specs + registry registration
  - [x] Unit tests with temp git repos

- [x] **3.5 Merge Base Detection** (S) — `6d6536d`
  - [x] `MergeBase()` and `DiffStat()` in `runtime/git/`
  - [x] Enhanced `view_diff` tool with `merge_base` flag
  - [x] Auto-detect main/master base branch
  - [x] Unit tests

---

## Deferred / Won't Do

| Item | Status | Reason |
|------|--------|--------|
| 1.2 Azure OpenAI | PUNT | OpenAI-compat adapter covers it |
| 1.3 JSONC Config | WONTDO | Current JSON config sufficient |
| 1.4 Config Validation | WONTDO | Current config handling sufficient |
| 2.4 TUI Session List | PUNT | Replaced by OTEL-integrated search |
| 3.3 In-app Summary Stats | PUNT | Replaced by OTEL metrics |
| 4.1 MCP SSE Transport | PUNT | Stdio transport covers primary cases |
| 4.2 MCP HTTP Transport | PUNT | Deferred with phase 4 |
| 4.3 MCP Resources/Prompts | PUNT | Deferred with phase 4 |
| 4.4 MCP OAuth | PUNT | Deferred with phase 4 |
| 4.5 Pattern Permission Rules | PUNT | Current system is functional |
| 4.6 Per-Session Permissions | PUNT | Current system is functional |

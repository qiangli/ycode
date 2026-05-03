# Gap Analysis: Aider — Built-in Tool System & Tool Use

**Tool:** Aider v0.x (Python, CLI agent, Apache-2.0 license)
**Domain:** Built-in Tool System & Tool Use
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | Aider |
|------|-------|-------|
| Tool count | 70+ tools with full handlers | ~40 slash commands (no tool-use API) |
| Tool discovery | Deferred tool activation with TTL, ToolSearch meta-tool | Static method-based discovery (cmd_* naming) |
| Execution tiers | 3-tier fallback (native Go → host exec → container) with gap recording | Single tier: subprocess via pexpect/subprocess |
| Browser automation | Container-based browser-use with multi-tab, DOM extraction | Playwright for web scraping only (no automation) |
| Web search | 5-provider chain (SearXNG → Brave → Tavily → DuckDuckGo) | No web search (only web scraping via /web) |
| Permission model | 3-tier modes + policy engine with command patterns + priority | Simple confirm_ask() prompts (boolean) |
| Shell execution | In-process mvdan/sh interpreter + security middleware | pexpect (Unix) / subprocess (Windows) |
| VFS | Symlink resolution, user prompting, device detection, 10MB limit | .aiderignore + gitignore only |
| Container tools | Generic containertool framework, auto-build Dockerfiles | No container support |
| MCP integration | Full client (stdio + SSE), server mode, tool bridging | No MCP support |
| Test runner | Multi-language (Go, Python, JS, Rust) with output parsing | Single configurable test command via /test |
| Git operations | 31 native go-git functions + tiered fallback | GitPython wrapper with attribution |
| Output management | Disk save, head/tail preview, ring buffer streaming, auto-cleanup | Real-time streaming only, no disk save |
| Lint integration | LintRunner with per-extension commands, MaxRetries, error parsing | Multi-linter (tree-sitter + flake8) with context display |

---

## Gaps Identified

| ID | Feature | Aider Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| T1 | Indentation-normalized fuzzy matching (RelativeIndenter) | Converts absolute to relative indentation; enables matching code blocks regardless of surrounding indent level | ycode fuzzy edit has line-trim + block-anchor but no indentation normalization | High | Low |
| T2 | Multiple edit format strategies | 8+ pluggable formats (SEARCH/REPLACE, udiff, whole-file, patch, fenced) selected per model | ycode uses single edit approach (exact + fuzzy); no format selection | Low | High |
| T3 | Lint context display with tree-sitter TreeContext | Shows surrounding code structure around lint errors using tree-sitter | ycode lint returns raw error output; no structural context | Low | Medium |
| T4 | Voice input via Whisper | Records audio, transcribes via OpenAI Whisper, uses as input | ycode has no voice input | Low | Medium |
| T5 | Clipboard image paste | /paste command imports images and text from clipboard | ycode has no clipboard integration | Low | Low |
| T6 | Streaming diff display during whole-file edits | Shows live diff as response streams in | ycode shows final result only | Low | Medium |

---

## Implementation Plan

### Phase 1: Indentation-Normalized Fuzzy Matching (T1)

**Files to modify:**
- `internal/runtime/fileops/edit_fuzzy.go` — Add Level 1.5: indentation-normalized matching between line-trimmed and block-anchor

**Design:**
- Determine the minimum indentation (base indent) of the search text
- Strip base indent from search text to get "de-indented" version
- For each potential match location in content, determine local base indent
- Compare de-indented search against de-indented content section
- On match, return the original (indented) byte range from content

This handles the common case where the LLM produces correct code but at the wrong indentation level (e.g., produces 4-space indent when the actual code is at 8 spaces).

---

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| T2 | Multiple edit formats | Architectural redesign; ycode's single approach with fuzzy matching is adequate |
| T3 | Lint context display | Nice UX improvement but not critical; ycode's lint already returns line numbers |
| T4 | Voice input | Specialized input modality; not core tool system |
| T5 | Clipboard paste | Platform-specific UX; not core tool system |
| T6 | Streaming diff | Display concern; not tool system correctness |

---

## Verification

- Unit tests for indentation-normalized matching
- `make build` must pass
- Verify matching works for common indentation mismatch scenarios

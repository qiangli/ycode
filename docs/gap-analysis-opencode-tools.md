# Gap Analysis: OpenCode — Built-in Tool System & Tool Use

**Tool:** OpenCode v1.14.33 (TypeScript/Bun, MIT license)
**Domain:** Built-in Tool System & Tool Use
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | OpenCode |
|------|-------|----------|
| Tool count | 70+ tools with full handlers | ~15 built-in tools |
| Tool discovery | Deferred tool activation with TTL (8 turns), ToolSearch meta-tool | Feature-flag gated, model-specific filtering |
| Execution tiers | 3-tier fallback (native Go → host exec → container) with gap recording | Single tier (in-process) |
| Browser automation | Container-based browser-use with multi-tab, DOM extraction, screenshots | No native browser (relies on MCP) |
| Web search | 5-provider chain (SearXNG → Brave → Tavily → SearXNG URL → DuckDuckGo) | Single provider (Exa via MCP) |
| Permission model | 3-tier modes + policy engine with command-level pattern matching + priority | Wildcard rules with last-match-wins |
| Bash security | AST-based TTY detection, stall watchdog, ring buffer streaming, background jobs | Tree-sitter AST + dynamic content detection |
| Shell execution | In-process mvdan/sh interpreter + ExecHandler security middleware | Subprocess via PTY + Effect-based process spawning |
| VFS | Symlink resolution with user prompting, 10MB limit, device file detection | External directory checks only |
| Output management | Disk save at 50KB threshold, head/tail preview, ring buffer streaming | Dual-buffering + disk spillover + auto-cleanup directory |
| MCP integration | Full client (stdio + SSE), server mode (`ycode mcp serve`), tool bridging | Full client (stdio + SSE + HTTP), OAuth for authenticated servers |
| Test runner | Multi-language: Go, Python (pytest), JS (jest/vitest), Rust (cargo) | No dedicated test runner |
| Container tools | Generic containertool framework, auto-build Dockerfiles, mount system | No container tools |
| Git operations | 31 native go-git functions + tiered fallback | Git CLI wrapper |

---

## Gaps Identified

| ID | Feature | OpenCode Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| T1 | BOM/line-ending preservation in write/edit | Detects BOM and line endings; normalizes replacement text to match; preserves BOM across all operations | ycode has `DetectEncoding`/`DetectLineEndings` utilities but does NOT use them in `WriteFile`/`EditFile` | High | Low |
| T2 | Protected path filtering (macOS TCC) | Skips Music, Pictures, Movies, Downloads, Desktop, Documents, Library/*, Spotlight dirs during walks to prevent TCC permission dialogs | ycode's walker skips `.git`, `node_modules` etc but not OS-protected directories | Medium | Low |
| T3 | File not-found suggestions | When file not found, lists directory and suggests similar filenames via fuzzy matching | ycode returns error with hint "use read_file to verify" but no concrete suggestions | Medium | Low |
| T4 | Output directory auto-cleanup | Hourly background task removes saved output files older than 7 days from truncation directory | ycode saves large outputs to disk but never cleans them up | Medium | Low |
| T5 | LSP file warming | Touches files in LSP server on read/write to warm language server caches | ycode has LSP client but no warming-on-access pattern | Low | Low |
| T6 | Arity-based command intent extraction | 162 command prefixes mapped to token counts; extracts semantic command intent for permission display | ycode's policy engine uses full command patterns; no intent extraction for UI | Low | Medium |
| T7 | Formatter integration post-edit | Formats files immediately after writes/edits via LSP formatter | ycode doesn't auto-format after edits | Low | Medium |

---

## Implementation Plan

### Phase 1: File Operation Correctness (T1)

**Files to modify:**
- `internal/runtime/fileops/write.go` — Read existing file before writing; detect and preserve BOM + line endings
- `internal/runtime/fileops/edit.go` — Normalize replacement text to match file's line endings; preserve BOM

**Design:**
- On write: if file exists, read first bytes to detect encoding/BOM/line-endings. Normalize new content to match. Prepend BOM if original had one.
- On edit: detect line endings of file. If replacement text has different line endings, normalize to match.

### Phase 2: Platform Safety (T2)

**Files to create:**
- `internal/runtime/fileops/protected.go` — Platform-specific protected path list

**Files to modify:**
- `internal/runtime/fileops/walker.go` — Check protected paths during traversal

**Design:**
- On macOS: skip user-level directories that trigger TCC prompts (Music, Pictures, Movies, Downloads, Desktop, Documents, Library/*)
- Build-tagged for darwin only; no-op on other platforms

### Phase 3: UX Improvements (T3)

**Files to modify:**
- `internal/runtime/fileops/read.go` — On file-not-found, list directory and suggest similar names

**Design:**
- When `os.ReadFile` returns `ErrNotExist`, list the parent directory
- Compute Levenshtein or prefix similarity against available files
- Return top 3 suggestions in the error message

### Phase 4: Disk Hygiene (T4)

**Files to create:**
- `internal/runtime/bash/output_cleanup.go` — Background cleanup goroutine

**Design:**
- On startup, launch goroutine that runs every hour
- Remove files in the output directory older than 7 days
- Configurable via context (retention period)

---

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| T5 | LSP file warming | Minor optimization; ycode's LSP is functional without it |
| T6 | Arity-based command intent | ycode's policy engine pattern matching is adequate; cosmetic UI improvement only |
| T7 | Formatter integration | Risk of unwanted changes; should be opt-in feature, not default |

---

## Verification

- `make build` must pass after all changes
- Unit tests for BOM/line-ending preservation
- Unit tests for protected path filtering
- Unit tests for file suggestion
- Unit tests for cleanup scheduling

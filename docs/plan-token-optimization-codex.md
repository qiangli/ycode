# Token Optimization Plan: Lessons from Codex CLI for ycode

## Context

After Phases 1-13 (clawcode + aider + Kimi CLI), this plan addresses a fourth set of gaps revealed by studying OpenAI's Codex CLI. Codex takes a distinctly **API-level optimization** approach — leveraging ZSTD/gzip request compression, freeform tool grammar, and a "context checkpoint" compaction model.

---

## Phase 14: HTTP Request Body Compression (HIGH impact — 60-80% bandwidth reduction)

LLM API requests are JSON text — highly compressible. Codex uses ZSTD compression for HTTP requests.

### Design

- **New file:** `internal/api/compression.go` — `CompressGzip()` / `DecompressGzip()`
- Only compress requests > 4KB (below this, overhead exceeds savings)
- Integrated into `anthropic.go` and `openai_compat.go` request builders
- Sets `Content-Encoding: gzip` header

---

## Phase 15: Compact apply_patch Format (HIGH impact — ~50% fewer tokens for edits)

Codex's `apply_patch` uses a grammar-based format instead of JSON-wrapped unified diff:

```
*** Begin Patch
*** Update File: src/main.go
@@ func main()
-    fmt.Println("old")
+    fmt.Println("new")
*** End Patch
```

### Design

- **New file:** `internal/tools/patch_codex.go` — Codex-style patch parser
  - `ParseCodexPatch()` / `ApplyCodexPatch()` / `IsCodexPatch()`
  - Supports Add/Delete/Update file operations
  - Uses `@@` context hints (class/function names) instead of line numbers
- **File:** `internal/tools/patch.go` — auto-detect format (if starts with `*** Begin Patch`, use Codex parser)
- **File:** `internal/tools/specs.go` — updated description mentioning both formats

---

## Phase 16: Handoff Memo Compaction Framing (MEDIUM impact — better context preservation)

Codex frames compaction summaries as "context checkpoint handoffs between model instances."

### Design

- Updated `compactContinuationPreamble` to handoff framing
- Updated `llmSummaryPrompt` to "CONTEXT CHECKPOINT COMPACTION" framing
- Zero-cost optimization — same tokens, better LLM behavior

---

## Phase 17: Reasoning Effort Control (LOW impact)

### Design

- Added `ReasoningEffort string` to `api.Request` (values: "low", "medium", "high")
- Passed to providers when set

---

## Not Applicable to ycode (OpenAI-specific)

- **Incremental WebSocket requests** — Requires OpenAI Responses API with `previous_response_id`
- **Remote compaction endpoint** — OpenAI-specific `/responses/compact` API
- **Freeform tool type** — OpenAI-specific "custom tool" format with Lark grammar

---

## Implementation Status: COMPLETED

All four phases implemented and tested. Full test suite passing.

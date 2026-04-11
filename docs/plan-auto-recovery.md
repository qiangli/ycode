# Plan: Auto-Recovery from Token Limit Errors

## Context

When a conversation exceeds the model's context window, ycode must automatically recover without losing the user's work. This plan describes a multi-layered defense system that detects token limit errors, compacts conversation history, and retries — all transparently to the user.

The design is inspired by Claw Code's compaction approach but extends it with proactive compaction, pruning, observation masking, and an emergency flush fallback.

---

## Layer 0: Observation Masking

Oldest tool results outside a 10-message sliding window are replaced with `<MASKED>` placeholders. This is the lightest intervention — no summarization, just selective hiding of stale observations.

**File:** `internal/runtime/session/pruning.go`

- `MaskOldObservations()` — replaces old tool_result content with `<MASKED>`
- 10-message window preserved verbatim

---

## Layer 1: Pruning (Soft/Hard Context Trimming)

Tool result content is trimmed in-place without full compaction. Two severity tiers based on context health thresholds:

**File:** `internal/runtime/session/pruning.go`

| Tier | Threshold | Action |
|------|-----------|--------|
| Soft trim | 60% of CompactionThreshold | Keep head (400 chars) + tail (200 chars), insert omission note |
| Hard clear | 80% of CompactionThreshold | Replace entire content with `[Tool output pruned...]` |

**Supporting types:**
- `ContextHealth` struct — estimated tokens, threshold, ratio, and level (Healthy/Warning/Critical/Overflow)
- `CheckContextHealth()` — evaluates message set against budget
- `NeedsPruning()` / `NeedsCompactionNow()` — threshold checkers
- `PruneResult` — counts of soft-trimmed and hard-cleared results
- Last 6 messages are always protected from pruning

---

## Layer 2: Compaction (Proactive & Reactive)

### 2a: Proactive Compaction

Before making an API call, `TurnWithRecovery()` checks context health. If `NeedsCompactionNow()` returns true (>100% threshold), compaction fires before the request is sent — avoiding the API error entirely.

### 2b: Reactive Compaction

If the API returns a `TokenLimitError` (HTTP 400/413/422), the runtime:
1. Calls `proactiveCompactCtx()` to generate a structured summary
2. Rebuilds the message list: summary + last 4 preserved messages
3. Retries the API call with the compacted context

**Files:**
- `internal/runtime/conversation/runtime.go` — `TurnWithRecovery()`, `proactiveCompactCtx()`, `buildCompactedMessages()`
- `internal/runtime/session/compact.go` — `Compact()`, `CompactWithLLM()`, `buildIntentSummary()`

### Compaction Summary Structure

The heuristic summarizer (`buildIntentSummary()`) produces:
- Message counts by role
- Primary goal inference
- Verified facts from tool outcomes
- Working set (actively edited files)
- Active blockers (errors)
- Decision log
- Key files and tools used
- Pending work items

When an LLM summarizer is available, `CompactWithLLM()` delegates to it for higher-fidelity summaries with a 30-second timeout and 1024-token limit, falling back to heuristic on failure.

### Tool Pair Preservation

Compaction never splits `tool_use` / `tool_result` message pairs. The boundary is adjusted to keep pairs intact.

---

## Layer 3: Emergency Flush

Last resort when compaction still isn't enough. Creates a minimal continuation:
- Summary of entire conversation + last user message only
- Injects post-compaction context refresh from CLAUDE.md
- Sanitizes orphaned `tool_result` blocks from user messages
- Sets `recovery.Flushed = true`

**File:** `internal/runtime/conversation/runtime.go` — `emergencyFlush()`

---

## Error Detection

**File:** `internal/api/errors.go`

| Type | Description |
|------|-------------|
| `TokenLimitError` | Struct with `RequestedTokens`, `MaxTokens`, `Message` |
| `ParseTokenLimitError()` | Parses error strings from OpenAI, Anthropic, and generic formats |
| `IsTokenLimitError()` | Type assertion via `errors.As()` |

**Integration:** `internal/api/retry.go` checks HTTP 400/413/422 responses, parses token limit errors, and returns them immediately (no retry at HTTP level — recovery happens in the conversation runtime).

---

## Context Budget

**File:** `internal/runtime/session/budget.go`

Model-aware threshold calculation following Cline's proportional pattern:
- 32K context → 8K reserved
- 64K → 16K reserved
- 128K → 30K reserved
- 200K → 40K reserved
- &gt;200K → 20% reserved

Compaction triggers at the halfway point between reserved and total.

---

## CLI Integration

**Files:** `internal/cli/app.go`, `internal/cli/tui.go`

Both one-shot and TUI modes call `RunTurnWithRecovery()`. User notifications:
- Pruning: `⟳ Context pruned: N tool results trimmed to save context.`
- Compaction: `⚠ Context compacted: N messages summarized, M recent messages preserved.`
- Emergency flush: `⚠ Emergency context flush: conversation restarted with summary + last request.`

---

## OTEL Instrumentation

**File:** `internal/runtime/conversation/otel.go`

`InstrumentedTurnWithRecovery()` wraps the recovery flow in an OTEL span, recording compacted_count, preserved_count, and flushed status as span attributes.

---

## Comparison with Claw Code

| Aspect | Claw Code | ycode |
|--------|-----------|-------|
| Trigger | 100K token threshold (proactive) | Both proactive and reactive |
| Defense layers | 1 (compaction) | 4 (masking → pruning → compaction → flush) |
| Preserve count | 4 messages | 4 messages (compaction), 6 (pruning) |
| Summary format | Structured with priority tiers | Structured with XML tags + optional LLM |
| Multiple rounds | Yes | Emergency flush as fallback |
| User notification | Silent | Explicit messages at each layer |
| Observability | None | OTEL spans and metrics |

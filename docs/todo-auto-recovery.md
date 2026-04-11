# Auto-Recovery — Implementation Checklist

## Error Detection

### `internal/api/errors.go`
- [x] `TokenLimitError` struct with `RequestedTokens`, `MaxTokens`, `Message` fields
- [x] `ParseTokenLimitError()` — parses OpenAI, Anthropic, and generic error formats
- [x] `IsTokenLimitError()` — type assertion helper via `errors.As()`
- [x] `extractNumber()` — helper to parse token values from error strings

### `internal/api/errors_test.go`
- [x] Tests for OpenAI-style error format
- [x] Tests for Anthropic-style error format
- [x] Tests for generic error formats ("context length exceeded", "too large", "too long")
- [x] Tests for `IsTokenLimitError()` with wrapped and nil errors
- [x] Tests for number extraction patterns

### `internal/api/retry.go`
- [x] Check HTTP 400/413/422 responses for token limit errors
- [x] Call `ParseTokenLimitError(body)` on matching status codes
- [x] Return `TokenLimitError` immediately (bypass HTTP retry)

## Context Budget

### `internal/runtime/session/budget.go`
- [x] `ContextBudget` type with model-aware threshold calculation
- [x] Proportional token reservation (32K→8K, 64K→16K, 128K→30K, 200K→40K, >200K→20%)
- [x] Compaction trigger at halfway point between reserved and total

### `internal/runtime/session/budget_test.go`
- [x] Tests for context budget calculations across model sizes

## Pruning (Layer 0 + Layer 1)

### `internal/runtime/session/pruning.go`
- [x] `MaskOldObservations()` — observation masking with 10-message window
- [x] `PruneMessages()` — soft trim (head 400 + tail 200 chars) and hard clear
- [x] `ContextHealth` struct with EstimatedTokens, Threshold, Ratio, ContextLevel
- [x] `CheckContextHealth()` — evaluate message set against budget
- [x] `NeedsPruning()` — check Warning threshold (60%)
- [x] `NeedsCompactionNow()` — check Overflow threshold (>100%)
- [x] `PruneResult` with `SoftTrimmed` and `HardCleared` counts
- [x] Protection of last 6 messages from pruning

### `internal/runtime/session/pruning_test.go`
- [x] Tests for soft trimming behavior
- [x] Tests for hard clearing behavior
- [x] Tests for observation masking

## Compaction (Layer 2)

### `internal/runtime/session/compact.go`
- [x] `Compact()` — core heuristic compaction with 4-message preservation
- [x] `CompactWithLLM()` — LLM-based alternative with heuristic fallback
- [x] `buildIntentSummary()` — structured summary (counts, goal, facts, working set, blockers, decisions, files, tools, pending work)
- [x] `GetCompactContinuationMessage()` — format summary for model consumption with preamble
- [x] `NeedsCompaction()` — threshold check (>100K tokens)
- [x] Tool pair preservation (never split `tool_use`/`tool_result`)
- [x] Merge with previous summaries for hierarchical context

### `internal/runtime/session/compact_test.go`
- [x] Tests for compaction logic
- [x] Tests for summary building
- [x] Tests for tool pair preservation
- [x] Tests for previous summary merging

### `internal/runtime/session/llm_summary.go`
- [x] `LLMSummarizer` type with 30-second timeout and 1024-token limit
- [x] Structured prompt for consistent summary format
- [x] Fallback wrapping in `<intent_summary>` tags if missing

### `internal/runtime/session/llm_summary_test.go`
- [x] Tests for LLM summarization

## Recovery Orchestration (Layer 2 + Layer 3)

### `internal/runtime/conversation/runtime.go`
- [x] `RecoveryResult` struct (CompactedCount, PreservedCount, RetrySuccessful, SummaryPreview, Pruned, PrunedCount, Flushed)
- [x] `TurnWithRecovery()` — main recovery orchestrator
- [x] Proactive phase: check context health before API call, compact if needed
- [x] Pruning integration: call `PruneMessages()` when context is Warning/Critical
- [x] Reactive phase: catch `TokenLimitError`, compact, rebuild messages, retry
- [x] `proactiveCompactCtx()` — call session compaction with optional LLM summarizer
- [x] `buildCompactedMessages()` — rebuild API messages with summary + preserved messages
- [x] `emergencyFlush()` — last resort: summary + last user message only
- [x] `sanitizeUserMessageForFlush()` — remove orphaned `tool_result` blocks
- [x] Post-compaction context refresh from CLAUDE.md
- [x] `apiMessagesToSession()` / `sessionMessagesToAPI()` — type conversion helpers

### `internal/runtime/conversation/runtime_test.go`
- [x] Tests for `sanitizeUserMessageForFlush()` behavior

## CLI Integration

### `internal/cli/app.go`
- [x] `RunTurnWithRecovery()` public API method
- [x] Agentic loop calls `TurnWithRecovery()` in `RunPrompt()`
- [x] Pruning notification: `⟳ Context pruned: N tool results trimmed to save context.`
- [x] Compaction notification: `⚠ Context compacted: N messages summarized, M recent messages preserved.`
- [x] Emergency flush notification: `⚠ Emergency context flush: conversation restarted with summary + last request.`

### `internal/cli/tui.go`
- [x] TUI interactive mode calls `RunTurnWithRecovery()`
- [x] Tool result turn uses recovery
- [x] `turnResultMsg` carries result + recovery info
- [x] Recovery display message shown to user

## OTEL Instrumentation

### `internal/runtime/conversation/otel.go`
- [x] `InstrumentedTurnWithRecovery()` — wraps recovery in OTEL span
- [x] Records compacted_count, preserved_count, flushed status as span attributes

## Documentation

- [x] `docs/auto-recovery.md` — user-facing documentation
- [x] `docs/plan-auto-recovery.md` — architecture plan
- [x] `docs/todo-auto-recovery.md` — this checklist

# Token Optimization Plan: Lessons from Kimi CLI for ycode

## Context

After Phases 1-9 (clawcode + aider), this plan addresses a third set of gaps revealed by studying Kimi CLI. Kimi CLI is especially relevant because **Moonshot/Kimi doesn't support Anthropic-style prompt caching** — it must rely entirely on message compression and smart context management.

---

## Phase 10: Adjacent User Message Merging (HIGH impact — ~100+ tokens/turn)

Kimi CLI's `normalize_history()` merges adjacent user messages before each API call. Each separate message has structural overhead (role tokens, boundary framing).

### Design

- **File:** `internal/runtime/session/normalize.go` — new `MergeAdjacentUserMessages()`
- Merge consecutive user-role messages by concatenating content blocks
- Never merge messages containing `ContentTypeToolResult`
- Called in `TurnWithRecovery()` after masking, before API call

---

## Phase 11: Dual Compaction Trigger (MEDIUM impact)

Kimi CLI uses dual trigger: ratio-based AND reserved-buffer.

### Design

- **File:** `internal/runtime/session/budget.go`
  - Add `ReservedBuffer int` (10% of context window)
  - Add `ShouldCompact(currentTokens int) bool` — fires on either condition

---

## Phase 12: Tool Visibility Toggling (MEDIUM impact)

Kimi CLI's `Toolset` has `hide()`/`unhide()` methods.

### Design

- **File:** `internal/tools/filtered.go`
  - Add `Hide(name string)` and `Unhide(name string)` to `FilteredRegistry`
  - Add `hidden map[string]bool` field
  - `isAllowed()` checks both allowlist and hidden set

---

## Phase 13: Thinking Content Stripping in Compaction (LOW impact)

Kimi CLI strips `ThinkPart` content during compaction.

### Design

- **File:** `internal/runtime/session/llm_summary.go`
  - `isThinkingContent()` detects `<thinking>`, `<antThinking>`, `<reasoning>` blocks
  - Skipped in `formatMessagesForSummary()`

---

## Implementation Status: COMPLETED

All four phases implemented and tested. Full test suite passing.

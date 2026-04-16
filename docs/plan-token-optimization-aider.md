# Token Optimization Plan: Lessons from Aider for ycode

## Context

After implementing Phase 1-4 from the clawcode analysis, this plan addresses a second set of gaps revealed by studying aider's architecture. Aider takes a fundamentally different approach to token optimization — where clawcode focuses on *compressing what's already in context*, aider focuses on *never putting unnecessary tokens in context in the first place*.

**Key insight:** Aider does NOT use tool/function calling at all. It uses structured text output parsing instead. This eliminates ~2-5KB of tool schema overhead per request. ycode uses tools (and must, for its architecture), but aider's other optimizations are directly applicable.

---

## Phase 5: Weak Model for Summarization (HIGH impact — 60-90% cost reduction on compaction)

ycode's `LLMSummarizer` always uses the main model. Aider uses a fallback chain: try weak model first, fall back to main model.

### Design

- **Config:** Add `WeakModel string` field (e.g., `claude-haiku-4-5-20251001`, `gpt-4o-mini`)
- **LLMSummarizer:** Accept list of `ModelSpec` pairs, try each in order
- **Wiring:** Build summarizer with `[weakModel, mainModel]` chain

### Token savings: 93% cheaper compaction (Haiku vs Opus)

---

## Phase 6: Proportional Chat History Budget (HIGH impact)

Aider's formula: `max_chat_history_tokens = min(max(max_input_tokens / 16, 1024), 8192)`

### Design

- Add `MaxChatHistoryTokens` to `ContextBudget`
- For non-caching providers: `min(max(contextWindow/24, 1024), 4096)`
- Enforce cap in `Compact()` and `CompactWithLLM()` via `EnforceSummaryCap()`

---

## Phase 7: Cache Warming for Prompt Caching Providers (MEDIUM impact)

Anthropic's prompt cache has a 5-minute TTL. Background pings keep it alive.

### New file: `internal/api/cache_warmer.go`

- Pings every 4.5 minutes with minimal request (`max_tokens=1`)
- Only active when `cachingSupported && config.CacheWarmingEnabled`

---

## Phase 8-9: Summary Cap + Recursive Summarization (LOW impact)

- `EnforceSummaryCap()` truncates summaries exceeding history budget
- Head/tail splitting preserves recent context (2/3 tail, 1/3 head)
- Both `Compact()` and `CompactWithLLM()` accept optional `maxHistoryTokens`

---

## Implementation Status: COMPLETED

All five phases implemented and tested. Full test suite passing.

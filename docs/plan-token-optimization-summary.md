# Token Optimization: Complete Summary

20 phases across 5 prior art studies. All implemented and tested.

## Prior Art Sources

| Source | Phases | Philosophy |
|--------|--------|-----------|
| [clawcode](plan-token-optimization-clawcode.md) | 1-4 | Compress what's in context |
| [aider](plan-token-optimization-aider.md) | 5-9 | Never put unnecessary tokens in |
| [Kimi CLI](plan-token-optimization-kimicli.md) | 10-13 | Structural compression for no-cache providers |
| [Codex CLI](plan-token-optimization-codex.md) | 14-17 | API-level and wire-format optimization |
| [Gemini CLI](plan-token-optimization-geminicli.md) | 18-20 | Production-hardened context management |

## All Phases

| # | Phase | Source | Impact | Status |
|---|-------|--------|--------|--------|
| 1 | Deferred tool loading (ToolSearch + activated tracking) | clawcode | HIGH | Done |
| 2 | Completion cache (30s TTL, disk-backed) | clawcode | MEDIUM | Done |
| 3 | Provider-aware aggressive distillation | clawcode | MEDIUM | Done |
| 4 | Provider-aware observation masking window | clawcode | LOW | Done |
| 5 | Weak model fallback chain for summarization | aider | HIGH | Done |
| 6 | Proportional chat history budget (input/16) | aider | HIGH | Done |
| 7 | Cache warming (background pings every 4.5min) | aider | MEDIUM | Done |
| 8-9 | Summary cap enforcement + recursive support | aider | LOW | Done |
| 10 | Adjacent user message merging | Kimi CLI | HIGH | Done |
| 11 | Dual compaction trigger (ratio + reserved buffer) | Kimi CLI | MEDIUM | Done |
| 12 | Tool visibility toggling (Hide/Unhide) | Kimi CLI | MEDIUM | Done |
| 13 | Thinking content stripping in compaction | Kimi CLI | LOW | Done |
| 14 | HTTP request body gzip compression | Codex CLI | HIGH | Done |
| 15 | Codex-style compact apply_patch format | Codex CLI | HIGH | Done |
| 16 | Handoff memo compaction framing | Codex CLI | MEDIUM | Done |
| 17 | Reasoning effort control | Codex CLI | LOW | Done |
| 18 | CJK-aware token estimation | Gemini CLI | HIGH | Done |
| 19 | Token-budget masking with protection window | Gemini CLI | HIGH | Done |
| 20 | Proportional truncation ratios (15/85) | Gemini CLI | LOW | Done |

## Files Created

| File | Purpose |
|------|---------|
| `internal/api/completion_cache.go` | 30s TTL disk-backed completion cache |
| `internal/api/cache_warmer.go` | Background pings to keep prompt cache alive |
| `internal/api/compression.go` | Gzip compression for HTTP request bodies |
| `internal/tools/patch_codex.go` | Codex-style compact patch parser |
| `internal/tools/deferred_test.go` | Tests for ToolSearch full schema output |
| `internal/api/completion_cache_test.go` | Completion cache tests |
| `internal/api/cache_warmer_test.go` | Cache warmer tests |
| `internal/api/compression_test.go` | Compression tests |
| `internal/tools/patch_codex_test.go` | Codex patch parser tests |

## Key Modified Files

| File | Changes |
|------|---------|
| `internal/tools/specs.go` | ToolSearch/Skill AlwaysAvailable, apply_patch description |
| `internal/tools/deferred.go` | Full schema output from ToolSearch |
| `internal/tools/filtered.go` | Hide/Unhide for dynamic tool visibility |
| `internal/tools/patch.go` | Auto-detect Codex vs unified diff format |
| `internal/runtime/conversation/runtime.go` | Activated tools, completion cache, cache warmer, message merging, budget-based masking |
| `internal/runtime/session/compact.go` | CJK-aware EstimateTextTokens, handoff framing, EnforceSummaryCap |
| `internal/runtime/session/pruning.go` | Token-budget masking, exempt tools, proportional truncation |
| `internal/runtime/session/budget.go` | MaxChatHistoryTokens, ReservedBuffer, ShouldCompact |
| `internal/runtime/session/distill.go` | AggressiveMode with halved thresholds |
| `internal/runtime/session/routing.go` | Aggressive routing thresholds, proportional partialContent |
| `internal/runtime/session/normalize.go` | MergeAdjacentUserMessages |
| `internal/runtime/session/llm_summary.go` | Weak model chain, thinking stripping, checkpoint framing |
| `internal/runtime/config/config.go` | WeakModel, CacheWarmingEnabled |
| `internal/api/types.go` | ReasoningEffort field |
| `internal/api/anthropic.go` | Gzip compression |
| `internal/api/openai_compat.go` | Gzip compression |
| `internal/cli/app.go` | Weak model wiring, cache warmer setup |

## Impact Summary by Provider Type

### Caching Providers (Anthropic)
- Cache warming prevents eviction during idle (~$0.0006/ping)
- Deferred tools save ~10KB/turn in tool schemas
- Completion cache eliminates redundant API calls
- Handoff memo framing improves post-compaction behavior

### Non-Caching Providers (OpenAI, Moonshot/Kimi, Gemini)
- Aggressive distillation halves inline thresholds
- Tighter masking budgets (60% of default)
- Earlier compaction via NonCachingCompactionDiscount
- Adjacent message merging saves ~100+ tokens/turn
- Gzip compression reduces bandwidth 60-80%
- Compact apply_patch saves ~50% on edit tokens

### CJK Users (all providers)
- CJK-aware estimation prevents 4-5x underestimation
- Compaction triggers at correct thresholds instead of allowing overflow

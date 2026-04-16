# Token Optimization Plan: Lessons from Gemini CLI for ycode

## Context

After Phases 1-17 (clawcode + aider + Kimi CLI + Codex CLI), this plan addresses a fifth set of gaps from Google's Gemini CLI. Gemini CLI has the most **production-hardened context management** of all prior art studied — a declarative profile system, three-layer compression pipeline, batched tool output masking with protection windows, and CJK-aware token estimation.

---

## Phase 18: CJK-Aware Token Estimation (HIGH impact)

ycode's `len(text)/4` heuristic severely underestimates tokens for CJK text (4-5x).

### Design

- **File:** `internal/runtime/session/compact.go` — new `EstimateTextTokens()`
- ASCII chars (0-127): 0.25 tokens/char
- Non-ASCII (CJK, etc.): 1.3 tokens/char
- Strings > 100K chars: fast `len/4` fallback for performance

---

## Phase 19: Token-Budget Masking with Protection Window (HIGH impact)

Replaces count-based masking with Gemini CLI's "Hybrid Backward Scanned FIFO":

1. **Protection window:** newest 50K tokens of tool outputs protected
2. **Batch threshold:** only mask if >30K tokens are prunable
3. **Exempt tools:** `AskUserQuestion`, `MemosStore`, `MemosSearch`, `EnterPlanMode`, `ExitPlanMode`, `Skill` never masked

### Design

- **File:** `internal/runtime/session/pruning.go`
  - New `MaskOldObservationsBudget()` with token-budget approach
  - `ExemptFromMasking` set for high-signal tools
  - Non-caching providers: tighter budgets (60% of defaults)
- **File:** `internal/runtime/conversation/runtime.go` — uses budget-based masking

### Why better than count-based
A 10K-token file read and a 50-token write confirmation are treated equally by count-based masking. Token-budget masking protects important recent results regardless of count.

---

## Phase 20: Proportional Truncation Ratios (LOW impact)

### Design

- Changed from fixed 400 head / 200 tail to ratio-based: 15% head / 85% tail
- Total budget stays at 600 chars
- Head: 90 chars, Tail: 510 chars
- Tail is more valuable (error messages, final results)

---

## Already Implemented (no action needed)

- 4-layer context defense (mask → prune → compact → flush) — more robust than Gemini's 3 layers
- Tool output distillation with routing decisions
- Thinking content stripping (Phase 13)
- Content routing cache with SHA256 hashing
- Provider-aware budgets

---

## Implementation Status: COMPLETED

All three phases implemented and tested. Full test suite passing.

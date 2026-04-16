# Token Optimization Plan: Closing Gaps Between ycode and clawcode

## Context

ycode already has a solid token optimization foundation (4-layer context defense, deferred tool architecture, differential prompt injection, provider-aware budgets). However, comparing against clawcode reveals several gaps where ycode leaves tokens on the table — especially for non-caching providers (Moonshot/Kimi, OpenAI, Gemini). This plan addresses the highest-impact gaps.

**Key finding:** ycode's deferred tool architecture is *structurally* in place (only 6 `AlwaysAvailable` tools are sent per request) but *functionally broken* — `ToolSearch` is not marked always-available and returns only names, not schemas. Fixing this is the #1 priority.

---

## Phase 1: Fix Deferred Tool Loading (HIGH impact — ~10KB/turn savings)

The single biggest win. Tool schemas are verbose JSON. Sending 35+ deferred tool schemas every turn wastes ~10KB. ycode already filters to `AlwaysAvailable` tools in `Turn()` but the ToolSearch meta-tool is broken.

### 1a. Mark meta-tools as AlwaysAvailable

**File:** `internal/tools/specs.go`
- Add `AlwaysAvailable: true` to `ToolSearch` and `Skill`
- These are tiny schemas (~200 bytes each) and essential for agent operation

### 1b. Make ToolSearch return full tool schemas

**File:** `internal/tools/deferred.go`
- Current output: `- ToolName (score: N): description` (useless — LLM can't call the tool)
- New output: full JSON schema for each matched tool (`name`, `description`, `input_schema`)
- Format as a `<functions>` block matching clawcode's pattern

### 1c. Track activated deferred tools for API requests

**File:** `internal/runtime/conversation/runtime.go`
- Add `activatedTools map[string]bool` field to `Runtime`
- After processing a `ToolSearch` tool result, parse tool names from the result and add to `activatedTools`
- In `Turn()`, build `toolDefs` from `AlwaysAvailable()` PLUS any activated deferred tools

---

## Phase 2: Completion-Level Cache (MEDIUM impact — eliminates redundant API calls)

ycode has prompt fingerprinting for cache break detection but no completion cache.

### New file: `internal/api/completion_cache.go`

- `CompletionCache` struct with disk-backed storage
- Request hash: FNV-1a of `model + system_hash + tools_hash + messages_hash`
- TTL: 30 seconds (configurable)
- Restricted file permissions (0600)

---

## Phase 3: Provider-Aware Aggressive Distillation (MEDIUM impact)

Non-caching providers pay full price for every input token every turn.

### File: `internal/runtime/session/distill.go`

Add `AggressiveMode bool` to `DistillConfig`. When true:

| Threshold | Normal | Aggressive |
|-----------|--------|------------|
| MaxInlineChars | 2000 | 1000 |
| MaxInlineBytes | 50KB | 25KB |
| MaxInlineLines | 2000 | 1000 |

### File: `internal/runtime/session/routing.go`

Tighter thresholds when aggressive: search 500, bash 1500, default 1000.

---

## Phase 4: Provider-Aware Observation Masking (LOW impact)

### File: `internal/runtime/session/pruning.go`

`MaskOldObservations()` accepts window parameter; non-caching providers use window=6 (vs 10).

---

## Implementation Status: COMPLETED

All four phases implemented and tested. Full test suite passing.

# Phase 10: Best-in-Class Memory & Harness Improvements

## Source Analysis

Analyzed 7 reference implementations: Claw Code, OpenClaw, OpenCode, Aider, Cline, Continue, OpenHands.

## Prioritized Improvements (no external deps, high impact)

### 10.1 Loop/Stuck Detection (from Cline + OpenHands)
Both Cline (soft=3, hard=5) and OpenHands detect repetitive agent behavior.
**Implementation**: Track recent assistant responses, detect similarity, break infinite loops.
**Files**: New `internal/runtime/conversation/loop_detector.go`

### 10.2 LLM-Based Summarization Option (from Aider + OpenHands + Continue)
All top agents use LLM for compaction, not heuristics. Aider uses weak model.
**Implementation**: Add LLM summarization as upgrade to heuristic summary. Use the same provider.
**Files**: New `internal/runtime/session/llm_summary.go`, modify `compact.go`

### 10.3 Observation Masking (from OpenHands)
Replace old tool outputs with `<MASKED>` before full compaction — cheaper than pruning.
**Implementation**: Enhance existing pruning with masking as lightest-weight Layer 0.
**Files**: Modify `internal/runtime/session/pruning.go`

### 10.4 Agent-Requested Condensation (from OpenHands)
Agent can request its own memory compaction via a tool.
**Implementation**: New tool that triggers compaction from within the agentic loop.
**Files**: Modify tool registry

### 10.5 Model-Aware Context Budgets (from Cline)
Cline adjusts reserved tokens per model context window (27K-40K).
**Implementation**: Dynamic thresholds based on model's max_input_tokens.
**Files**: Modify `internal/runtime/session/pruning.go`, `compact.go`

## Implementation Order
1. Loop detection (safety feature, prevents wasted tokens)
2. Observation masking (lightest compaction layer)
3. Model-aware context budgets (better threshold tuning)
4. LLM-based summarization (quality improvement)

## Status: COMPLETED (2026-04-10)

All 4 improvements implemented with tests. See docs/todo.md Phase 10.

# Memory System Implementation Plan

## Overview

ycode's memory system was built across three implementation phases, adopting features from 9 reference implementations. This consolidated plan documents all 21 features organized by domain, their source tools, and implementation status.

**Phases consolidated:**
- Phase 1 (2026-04-10): Foundational 5-layer stack and 3-layer context defense
- Phase 2 (2026-04-11): 12 features adopted from Gemini CLI and Codex
- Phase 10 (2026-04-10): 4 best-in-class improvements from broader survey

## Reference Implementations

Each tool's full memory analysis is in `docs/memory/<tool>.md`.

| Tool | Primary Memory Strategy | Key Contribution to ycode |
|------|------------------------|--------------------------|
| [Aider](docs/memory/aider.md) | Structural git-maps (repo map via tree-sitter) | LLM-based summarization approach |
| [Claw Code](docs/memory/clawcode.md) | JSONL sessions with structured compaction | Base architecture — ycode is a Go rewrite of Claw Code |
| [Cline](docs/memory/cline.md) | Task-based session logs with permissioned loop | Loop/stuck detection thresholds, model-aware context budgets |
| [Codex](docs/memory/codex.md) | Differential context injection, ghost state | Differential injection, ghost snapshots, history normalization |
| [Continue](docs/memory/continue.md) | Extensible context providers with codebase RAG | LLM-based summarization approach |
| [Gemini CLI](docs/memory/geminicli.md) | 4-tier hierarchical memory, JIT loading, 1M+ context | JIT discovery, #import, intent summary, state snapshots, topic tracking, content routing, prewarming, hierarchical scopes |
| [OpenClaw](docs/memory/openclaw.md) | Persistent markdown (SOUL.md, MEMORY.md) | 3-layer context defense pattern, markdown-first philosophy |
| [OpenCode](docs/memory/opencode.md) | LSP-integrated terminal agent | Session JSONL format, compaction structure |
| [OpenHands](docs/memory/openhands.md) | 9-type condenser pipeline, microagents | Observation masking, agent-requested condensation |

## Feature Domains

### 1. Context Defense (3-Layer Stack + Layer 0)

Inspired by OpenClaw's architecture. Each layer is progressively more aggressive.

| Feature | Source | Status | Implementation |
|---------|--------|--------|----------------|
| **Layer 0: Observation masking** — Replace old tool outputs with `<MASKED>` before pruning | OpenHands | Done | `session/pruning.go` |
| **Layer 1: Context pruning** — Soft trim (60% threshold) truncates old tool results; hard clear (80%) replaces with placeholder | OpenClaw | Done | `session/pruning.go` |
| **Layer 2: Session compaction** — Structured intent summary replacing old messages | Claw Code | Done | `session/compact.go` |
| **Layer 3: Emergency memory flush** — Minimal continuation with summary + last user message | OpenClaw | Done | `conversation/runtime.go` |
| **Post-compaction context refresh** — Re-inject critical CLAUDE.md sections after compaction | OpenClaw | Done | `prompt/refresh.go` |
| **Model-aware context budgets** — Dynamic thresholds based on model's max_input_tokens | Cline | Done | `session/pruning.go`, `session/compact.go` |
| **Proactive auto-compaction** — Token estimation before each API call, compact proactively | OpenClaw | Done | `conversation/runtime.go` |
| **Context health monitoring** — Log context health metrics, warn at thresholds (Healthy/Warning/Critical/Overflow) | OpenClaw | Done | `session/pruning.go` |

### 2. Context Enrichment

| Feature | Source | Status | Implementation |
|---------|--------|--------|----------------|
| **Differential context injection** — For non-caching providers, only re-send changed dynamic sections via per-section hash baseline | Codex | Done | `prompt/baseline.go`, `prompt/builder.go` |
| **Provider capability detection** — Static lookup maps provider+model to capabilities; user override via config | Codex | Done | `api/capabilities.go` |
| **JIT subdirectory context loading** — Discover instruction files when tools access new paths; content-hash deduplication | Gemini CLI | Done | `prompt/jit.go` |
| **`#import` directive** — Instruction files support `#import <path>` with circular-reference detection, max depth 3 | Gemini CLI | Done | `prompt/import.go` |
| **Startup prewarming** — Run instruction file discovery and memory loading concurrently | Codex | Done | `prompt/prewarm.go` |
| **Active topic tracking** — Extract task from user messages, inject `[Active Topic: ...]` into system prompt, clear after 20 turns | Gemini CLI | Done | `prompt/topic.go` |

### 3. Compaction Quality

| Feature | Source | Status | Implementation |
|---------|--------|--------|----------------|
| **Structured intent summary** — 5-category summary: Primary Goal, Verified Facts, Working Set, Active Blockers, Decision Log | Gemini CLI | Done | `session/compact.go` |
| **Ghost snapshots** — Serialize pre-compaction state to disk for debugging; never sent to model | Codex | Done | `session/ghost.go` |
| **Cumulative state snapshots** — Updated (not appended) on each compaction; tracks goal, steps, files, environment | Gemini CLI | Done | `session/state_snapshot.go` |
| **Summary compression** — Priority-tiered line selection within 1200-char / 24-line budget | Claw Code | Done | `session/compression.go` |

### 4. History Integrity

| Feature | Source | Status | Implementation |
|---------|--------|--------|----------------|
| **History normalization** — Enforce call-output pairing; synthesize missing tool_results, remove orphans | Codex | Done | `session/normalize.go` |
| **Tool output distillation** — Two-stage truncation with disk-backed full output; exempt tools bypass | Gemini CLI | Done | `session/distill.go` |
| **Per-file content routing** — Classify tool results as FULL/PARTIAL/SUMMARY/EXCLUDED during pruning | Gemini CLI | Done | `session/routing.go` |

### 5. Persistent Memory

| Feature | Source | Status | Implementation |
|---------|--------|--------|----------------|
| **Hierarchical memory scopes** — Global (`~/.ycode/memory/`) + project (`~/.ycode/projects/{hash}/memory/`) tiers; project ranked higher (1.1x) | Gemini CLI | Done | `memory/memory.go`, `memory/types.go` |
| **Typed staleness thresholds** — project: 30d, reference: 90d, user: 180d, feedback: 365d | OpenClaw | Done | `memory/age.go` |
| **Temporal decay scoring** — Logarithmic decay after 7 days: `score * 1/(1 + days/30)` | OpenClaw | Done | `memory/age.go` |
| **Background dreaming** — 30-min interval: stale removal + duplicate merging by normalized description | OpenClaw | Done | `memory/dream.go` |
| **MEMORY.md index** — Human-readable TOC, 200-line cap, upsert by filename | OpenClaw | Done | `memory/index.go` |
| **Multi-backend search** — Vector (chromem-go), full-text (Bleve BM25), keyword fallback | ycode original | Done | `memory/vectorindex.go`, `memory/bleveindex.go`, `memory/search.go` |

### 6. Safety & Reliability

| Feature | Source | Status | Implementation |
|---------|--------|--------|----------------|
| **Loop/stuck detection** — Track recent assistant responses, detect similarity, break infinite loops | Cline, OpenHands | Done | `conversation/loop_detector.go` |
| **Auto-checkpointing** — Compaction events trigger checkpoint with ID, label, summary | Claw Code | Done | `scratchpad/auto.go` |

### 7. Advanced Compaction

| Feature | Source | Status | Implementation |
|---------|--------|--------|----------------|
| **LLM-based summarization** — Optional LLM-based compaction summaries with heuristic fallback; enabled via `llmSummarizationEnabled` config flag | Aider, OpenHands, Continue | Done | `session/llm_summary.go`, `session/compact.go` (`CompactWithLLM`) |
| **Agent-requested condensation** — `compact_context` tool lets the agent trigger compaction on demand | OpenHands | Done | `tools/compact_context.go`, `conversation/runtime.go` (`CompactNow`) |

## Features Evaluated and Skipped

These features were considered during Phase 2 research and explicitly rejected.

| Feature | Source | Reason Skipped |
|---------|--------|---------------|
| Contextual fragment markers / multi-turn rollback | Codex | High complexity, marginal benefit given 3-layer defense |
| Memory manager sub-agent | Gemini CLI | Background Dreamer covers maintenance; sub-agent adds API cost |
| Encrypted reasoning items | Codex | Model-specific, not relevant to multi-provider approach |
| Pre-sampling compaction strategies | Codex | Already covered by 3-layer defense |

## Design Principles

1. **Markdown-first, no database** — All memory is `.md` with YAML frontmatter; filesystem is the database
2. **Human-editable** — Users can read, correct, or delete memories with any text editor
3. **Age-aware with type stratification** — Different memory types decay at different rates
4. **Relevance over recency** — Name matches (3x weight) can outrank more recent but poorly-named memories
5. **Tool-pair integrity** — Compaction never splits tool-use from tool-result
6. **Cache-friendly prompt structure** — Static above boundary, dynamic below; memory changes don't invalidate cache
7. **Provider-adaptive** — Caching providers use static/dynamic boundary; non-caching use differential injection
8. **Lazy context enrichment** — JIT discovery + `#import` for on-demand instruction loading
9. **Cumulative state, not append-only** — State snapshots updated per compaction; ghost snapshots preserve history
10. **History invariants** — Every tool_use has a tool_result; normalization enforces on load and before API calls

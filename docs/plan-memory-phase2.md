# Memory System Phase 2: Gemini CLI & Codex Feature Adoption

## Problem Statement

ycode's memory system (Phase 1) implemented the foundational 5-layer stack, 3-layer context defense, and background consolidation. Phase 2 studies the memory implementations of Gemini CLI (Google) and Codex (OpenAI) in `x/` and adopts their highest-impact features.

## Research Sources

- **Gemini CLI** (`x/gemini-cli/`): TypeScript CLI with 4-tier hierarchical memory, JIT context loading, `#import` directives, per-file content routing, intent summary tags, state snapshots, active topic tracking, tool output distillation, memory manager agent
- **Codex** (`x/codex/`): Rust CLI with differential context injection, ghost snapshots, history normalization (call-output invariant), contextual fragment markers, multi-turn rollback, startup prewarming

## Features Adopted (12)

### Phase 1: Context Enrichment

1. **Differential context injection** (Codex) — For non-caching providers (OpenAI, Ollama), maintain per-section hash baseline and only re-send changed dynamic sections between turns. Provider capabilities detected via static lookup with config override.

2. **JIT subdirectory context loading** (Gemini CLI) — When tools access files, discover instruction files from that directory up to project root. Thread-safe with content-hash deduplication.

3. **`#import` directive** (Gemini CLI) — Instruction files support `#import <path>` to inline other files. Circular-reference detection, max depth 3.

### Phase 2: Compaction Quality

4. **Structured intent summary** (Gemini CLI) — Replace 7-field text summary with `<intent_summary>` containing: Primary Goal, Verified Facts, Working Set, Active Blockers, Decision Log. Each extracted by dedicated heuristic helpers.

5. **Ghost snapshots** (Codex) — Serialize pre-compaction state to disk. Stored as `{sessionDir}/ghosts/{timestamp}.json`. Never sent to model; enables debugging.

6. **Cumulative state snapshots** (Gemini CLI) — `StateSnapshot` updated (not appended) on each compaction. Tracks goal, completed steps, working files, environment state.

### Phase 3: History Integrity

7. **History normalization** (Codex) — Enforce call-output pairing: synthesize missing `tool_result` for interrupted calls, remove orphan results.

8. **Tool output distillation** (Gemini CLI) — Two-stage heuristic: structural head/tail truncation, full output saved to disk. Exempt tools (read_file) bypass entirely.

### Phase 4: Prompt Enrichment

9. **Active topic tracking** (Gemini CLI) — Extract high-level task from user messages, inject `[Active Topic: ...]` into system prompt. Cleared after 20 turns.

10. **Hierarchical memory scopes** (Gemini CLI) — Global (`~/.ycode/memory/`) + project (`~/.ycode/projects/{hash}/memory/`) tiers. Manager queries both with project-scoped memories ranked higher.

### Phase 5: Advanced

11. **Per-file content routing** (Gemini CLI) — Classify tool results as FULL/PARTIAL/SUMMARY/EXCLUDED during pruning based on tool type, error status, and content size.

12. **Startup prewarming** (Codex) — Run instruction file discovery and memory loading concurrently with goroutines.

## Features Skipped (4)

| Feature | Source | Reason |
| :--- | :--- | :--- |
| Contextual fragment markers / multi-turn rollback | Codex | High complexity, marginal benefit given 3-layer defense |
| Memory manager sub-agent | Gemini CLI | Background Dreamer already covers maintenance; sub-agent adds API call cost |
| Encrypted reasoning items | Codex | Model-specific, not relevant to multi-provider approach |
| Pre-sampling compaction strategies | Codex | Already covered by existing 3-layer defense |

## New Files Created

| File | Purpose |
| :--- | :--- |
| `internal/api/capabilities.go` | Provider capability detection |
| `internal/runtime/prompt/baseline.go` | Context baseline for differential injection |
| `internal/runtime/prompt/jit.go` | JIT subdirectory context loading |
| `internal/runtime/prompt/import.go` | `#import` directive processing |
| `internal/runtime/prompt/topic.go` | Active topic tracking |
| `internal/runtime/prompt/prewarm.go` | Concurrent startup initialization |
| `internal/runtime/session/ghost.go` | Ghost snapshots |
| `internal/runtime/session/state_snapshot.go` | Cumulative state snapshots |
| `internal/runtime/session/normalize.go` | History normalization |
| `internal/runtime/session/distill.go` | Tool output distillation |
| `internal/runtime/session/routing.go` | Per-file content routing |

## Files Modified

| File | Changes |
| :--- | :--- |
| `internal/runtime/prompt/builder.go` | Added `BuildDifferential()`, `BuildDefault` now accepts caching/baseline params |
| `internal/runtime/prompt/discovery.go` | Integrated `ResolveImports()` after file loading |
| `internal/runtime/prompt/context.go` | Added `ProjectRoot`, `ActiveTopic` fields |
| `internal/runtime/session/compact.go` | Replaced `summarizeMessages()` with `buildIntentSummary()` |
| `internal/runtime/session/compression.go` | Added intent summary fields as P0 priority lines |
| `internal/runtime/memory/types.go` | Added `Scope` type and field |
| `internal/runtime/memory/memory.go` | Dual-store manager with `NewManagerWithGlobal()` |
| `internal/runtime/memory/store.go` | `scope` in frontmatter parsing/writing |
| `internal/runtime/conversation/runtime.go` | Differential context, baseline reset on compaction/flush |
| `internal/runtime/config/config.go` | Added `ProviderCapabilitiesConfig` |

## Design Principles

- All features maintain backward compatibility with existing session/memory formats
- No external databases or vector stores
- Provider-adaptive: capabilities detected automatically, overridable via config
- Distillation and normalization operate at execution time, not compaction time
- Ghost/state snapshots are additive (new files in new directories)

## Status: COMPLETED (2026-04-11)

All 12 features implemented, tested (`go test -race ./...` passes), and documented. See `docs/memory.md` for updated architecture documentation.

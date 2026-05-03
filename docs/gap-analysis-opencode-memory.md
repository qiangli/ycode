# Gap Analysis: OpenCode — Memory Management & Context Engineering

**Tool:** OpenCode v1.14.33 (TypeScript/Bun, MIT license)
**Domain:** Memory Management & Context Engineering
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | OpenCode |
|------|-------|----------|
| Memory architecture | 7-tier system: core types, store/manager, entity extraction, advanced retrieval, persona, extraction/consolidation, graph/temporal | Single-tier: SQLite sessions + file storage, no semantic layer |
| Memory types | 7 types (User, Feedback, Project, Reference, Episodic, Procedural, Task) × 4 scopes (Global, Project, Team, User) | No typed memory system; relies on session history |
| Retrieval | RRF fusion across 4 backends (vector, Bleve, keyword, entity) + MMR diversity re-ranking | Simple text search (`LIKE '%query%'`), no semantic search |
| Search backends | 4 backends: KV (bbolt), SQL (SQLite), Vector (chromem-go), Search (Bleve FTS) — all pure Go | SQLite only + legacy file storage |
| Persona modeling | Rich user profile: KnowledgeMap, CommunicationStyle, BehaviorProfile, SessionContext with adaptive confidence | No user modeling |
| Entity extraction | Named Entity Recognition, entity store, linking entities to memories | No entity extraction |
| Memory extraction | LLM-based + heuristic extraction from turns, background processing every 5 turns | Diff-based statistics only (additions, deletions, files modified) |
| Temporal validity | ValidFrom/ValidUntil dates, superseded-by references, staleness assessment | No temporal validity |
| Context defense layers | 5 layers: observation masking → soft trim → hard clear → distillation → compaction | 2 layers: tool output pruning → LLM compaction |
| Token counting | CJK-aware estimation (ASCII: 0.25 tok/char, CJK: 1.3 tok/char), fast approximation for large strings | Simple `text.length / 4` only |
| Context budget | Model-aware thresholds (6 tiers from 32K to 200K+), dual-trigger compaction, non-caching provider discount | Single formula: `context_limit - buffer - max_output`, 20K buffer |
| Prompt caching | SHA256 fingerprinting, background cache warming (4.5min keepalive), break detection, completion cache (30s TTL) | Provider-managed caching only, no warming or break detection |
| Prompt assembly | 19 named sections with static/dynamic boundary, differential mode for non-caching providers | Provider-specific base prompts + instructions + skills (simpler) |
| JIT discovery | Just-in-time CLAUDE.md/AGENTS.md discovery with deduplication, 100KB cap | Instruction file discovery with `claims` Map deduplication |
| Tool distillation | Multi-threshold (chars, bytes, lines), head/tail extraction, per-tool exemptions, disk save | Tool output truncation to 2000 chars during compaction |
| Memory prefetch | Async background memory search during tool execution (zero latency, 5s timeout) | No memory prefetch |
| Team memory | Parallel directory structure with path traversal protection, secret pattern detection | No team memory |
| Diagnostics | Duplicate file read detection, token waste calculation, context health levels (Healthy/Warning/Critical/Overflow) | Logging of compaction operations only |

---

## Gaps Identified

| ID | Feature | OpenCode Implementation | ycode Status | Priority | Effort |
|----|---------|------------------------|--------------|----------|--------|
| M1 | Replay mechanism for overflow recovery | When compaction still overflows, extracts last user message and replays it after compaction completes; handles cascading overflow | ycode has multi-layer defense but no replay-after-compaction recovery | Low | Medium |
| M2 | Observation marking with timestamps | Uses `time.compacted` timestamp on pruned tool outputs instead of deletion; reversible | ycode replaces with placeholder text (irreversible soft trim) | Low | Low |
| M3 | Provider-specific cache token accounting | Parses Anthropic, Vertex, and Bedrock metadata formats for cache_read/cache_write token breakdown | ycode tracks cache metrics for Anthropic; unclear multi-provider granularity | Low | Low |
| M4 | Session diff tracking | Stores file-level diffs (additions, deletions, files) per session step via snapshot service | ycode has session JSONL but no per-step diff summaries | Low | Medium |
| M5 | Auto-continuation after compaction | Plugin hook creates synthetic user message post-compaction ("Continue if you have next steps, or stop") | ycode has ContinuationPreamble but no synthetic message injection | Low | Low |

---

## Implementation Plan

**No actionable gaps identified.**

ycode's memory and context engineering system is vastly more sophisticated than OpenCode's. ycode implements a 7-tier memory architecture with 4 search backends, persona modeling, entity extraction, temporal validity, 5-layer context defense, intelligent caching with warming, and comprehensive diagnostics — none of which exist in OpenCode.

OpenCode's strengths are in its pragmatic simplicity: the replay mechanism (M1) is a clever recovery strategy, and timestamp-based observation marking (M2) is a nice reversibility approach. However, ycode's multi-layer defense system (observation masking → soft trim → hard clear → distillation → compaction) with 6 model-aware threshold tiers makes overflow situations extremely unlikely, rendering the replay mechanism unnecessary.

---

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| M1 | Replay mechanism | ycode's 5-layer defense makes overflow-during-compaction extremely unlikely; adding replay would be dead code |
| M2 | Timestamp marking | ycode's placeholder approach is simpler and the pruned content is rarely needed; disk save handles restoration |
| M3 | Multi-provider cache accounting | Minor observability improvement; can be added when other provider caching is prioritized |
| M4 | Session diff tracking | Nice for UI but not load-bearing; git history already provides this |
| M5 | Auto-continuation | ycode's ContinuationPreamble handles this case; synthetic messages add complexity |

---

## Verification

N/A — No implementation required.

---

## Summary

OpenCode implements a pragmatic, well-engineered context management system focused on the core problem: prevent overflow, summarize history, preserve recent context. Its multi-layer compaction (summarize → prune → replay) and the replay recovery mechanism are clever designs.

However, ycode's memory system operates at a fundamentally different scale of sophistication with 7 memory tiers, 4 search backends, persona modeling, entity linking, temporal validity, 5-layer context defense, cache warming, and comprehensive diagnostics. No actionable gaps were identified — every feature OpenCode has is either already present in ycode or superseded by a more comprehensive approach.

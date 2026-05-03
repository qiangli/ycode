# Gap Analysis: LangGraph — Memory Management & Context Engineering

**Tool:** LangGraph (Python framework for stateful multi-agent systems)
**Source:** `priorart/langgraph/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | LangGraph |
|------|-------|-----------|
| Three-layer pruning | Observation masking → soft trim → hard clear before compaction | No pruning layers; relies on full state snapshots |
| CJK-aware token estimation | ASCII 0.25 tokens/char, non-ASCII 1.3 tokens/char | Basic len() only |
| Prompt cache warming | Background keep-alive pings (4.5min vs 5min TTL) | No cache management |
| Microcompaction | Deterministic tool I/O clearing without LLM | No intermediate compaction layer |
| Entity extraction | EntityIndex.Link connecting entities to memories | No knowledge graph |
| JIT instruction discovery | Walks filesystem on-demand for instruction files | Static configuration only |
| Completion response caching | File + memory cache keyed by request hash (30s TTL) | Checkpoint state only |
| Multi-backend search fusion | RRF + MMR re-ranking across Bleve + vector + keyword + entity | Single-backend queries |
| Background memory extraction | Post-turn LLM extraction with configurable timing | No background extraction |

## Gaps Identified

| ID | Feature | LangGraph Implementation | ycode Status | Priority | Effort |
|----|---------|--------------------------|--------------|----------|--------|
| M1 | Memory TTL with background sweep | Store items have `ttl_minutes`, `expires_at`; background sweep thread at configurable interval cleans expired entries | ycode memories have no TTL or expiration; stale memories accumulate indefinitely | High | Medium |
| M2 | Namespace wildcard queries | `list_namespaces()` with prefix/suffix tuple matching for flexible collection discovery | ycode uses direct path lookups; no wildcard namespace queries | Low | Low |
| M3 | Store batch atomicity | `batch()` groups multiple get/put/search/delete into atomic operation | ycode storage has no multi-operation batch guarantees | Medium | Medium |
| M4 | Delta checkpoint encoding | `versions_seen` tracks what each node has seen; ancestor walks reconstruct state from deltas | ycode checkpoints are full snapshots; no delta encoding | Low | High |

## Implementation Plan

### Phase 1: Memory TTL & Background Sweep (M1)

**Files to modify:**
- `internal/runtime/memory/memory.go` — add TTL field to memory entries, sweep goroutine
- `internal/runtime/memory/types.go` — extend `MemoryEntry` with `ExpiresAt` and `TTLMinutes`
- `internal/storage/storage.go` — add `DeleteExpired(ctx, before time.Time)` to store interfaces

**Design:**
- Add `ExpiresAt *time.Time` and `TTLMinutes int` to `MemoryEntry`
- On write: if TTLMinutes > 0, set ExpiresAt = now + TTL
- Background sweep goroutine: runs every `SweepInterval` (default 15min), deletes entries where ExpiresAt < now
- Sweep is opt-in: only starts if TTL is configured in settings
- SQLite: add `expires_at` column, indexed for efficient range deletes

### Phase 2: Store Batch Operations (M3)

**Files to modify:**
- `internal/storage/storage.go` — add `BatchOp` type and `Batch(ctx, []BatchOp)` method
- `internal/storage/sqlite.go` — implement batch within a single transaction

**Design:**
- `BatchOp` is a union type: Put, Delete, or Search
- SQLite implementation wraps in `BEGIN/COMMIT` for atomicity
- KV store (bbolt) already has transaction support; expose it

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| M2 | Namespace wildcard queries | ycode's direct path + Bleve full-text search covers discovery use cases |
| M4 | Delta checkpoint encoding | High effort for marginal benefit; ycode's file-based checkpoints are simple and effective for single-machine use |

## Verification

- `make build` passes with no errors
- Unit tests for TTL expiration and sweep behavior
- Unit tests for batch atomicity (partial failure rolls back)
- Existing memory tests continue to pass

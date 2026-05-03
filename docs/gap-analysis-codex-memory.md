# Gap Analysis: Codex — Memory Management & Context Engineering

**Tool:** OpenAI Codex CLI (Rust, Apache-2.0 license)
**Domain:** Memory Management & Context Engineering
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | Codex |
|------|-------|-------|
| Memory architecture | 7-tier with types, scopes, persona, entities, temporal validity | Thread store + state DB + compaction (3 layers) |
| Memory types | 7 types × 4 scopes | Single raw markdown format |
| Retrieval | RRF fusion across 4 backends + MMR re-ranking | No semantic retrieval; usage-weighted ranking only |
| Search backends | KV, SQL, Vector, Bleve FTS (all pure Go) | State DB (SQLite) only |
| Context defense | 5 layers (masking → trim → clear → distill → compact) | Single compaction layer (inline or remote) |
| Token counting | CJK-aware, fast approximation | BASE64-based reasoning estimation, byte-level approx |
| Prompt assembly | 19 named sections with static/dynamic boundary, differential mode | ~25 fragment types (comparable but no caching boundary) |
| Prompt caching | SHA256 fingerprinting, warming (4.5min), break detection, completion cache | No explicit prompt caching strategy |

## Gaps Identified

| ID | Feature | Codex Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| M1 | Two-phase memory pipeline | Phase 1: per-rollout extraction (parallel), Phase 2: global consolidation (serialized with git diff) | ycode has extraction + consolidation but no git-based diff tracking | Low | Medium |
| M2 | Incremental context diffing | reference_context_item tracks baseline; only changed fragments re-injected | ycode has differential mode for non-caching providers (similar) | Low | N/A |
| M3 | Fragment-based context composition | ~25 distinct fragment types with independent versioning | ycode has 19 named sections (comparable) | Low | N/A |

## Implementation Plan

**No actionable gaps identified.** ycode's memory system is more comprehensive at every layer. Codex's two-phase pipeline is well-engineered but ycode's 7-tier architecture with 4 search backends supersedes it.

---

## Summary

Codex's memory system prioritizes reliability (watermarking, lease coordination) and observability (metrics). Its two-phase extraction pipeline is elegant but operates on raw markdown without semantic indexing. ycode's multi-tier architecture with vector search, entity linking, and temporal validity provides fundamentally richer capabilities.

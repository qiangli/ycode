# Gap Analysis: ADK-Python — Memory Management & Context Engineering

**Tool:** Google Agent Development Kit (ADK-Python)
**Source:** `priorart/adk-python/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | ADK-Python |
|------|-------|------------|
| Progressive compression | 4-layer cascade (masking → trim → microcompact → LLM) | No compaction; events append unbounded |
| CJK-aware token estimation | ASCII 0.25 tokens/char, non-ASCII 1.3 tokens/char | No token estimation |
| Post-compaction recovery | FileTracker restores recently-edited files after summary | No file restoration |
| Prompt cache management | Fingerprinting, break detection, background warming | No cache management |
| Multi-backend search fusion | RRF + MMR re-ranking across Bleve + vector + keyword + entity | Single-backend only (keyword or RAG) |
| Entity extraction & linking | EntityIndex.Link connecting entities to memories | No entity linking |
| Background memory extraction | Async extraction recognizing corrections/decisions | Manual memory writes only |
| Memory TTL & sweep | Background sweeper removes expired memories | No TTL support |

## ADK-Python Memory Features

ADK implements a pluggable memory architecture:
- **Session services**: Multi-backend (SQLite, in-memory, Vertex AI) with structured event history
- **State scoping**: `app:*`, `user:*`, `session:*`, `temp:*` prefixes for multi-tenant isolation
- **Memory services**: Pluggable backends (in-memory keyword, Vertex AI RAG semantic)
- **Incremental ingestion**: `add_events_to_memory()` for post-turn delta processing
- **Artifacts**: Versioned storage with metadata and MIME types (filesystem, GCS, in-memory)

## Gaps Identified

No actionable gaps identified. ycode's memory and context engineering is significantly ahead. ADK's state scoping (`app:*/user:*/session:*/temp:*`) is a clean pattern but ycode already has global/project scoping with equivalent isolation.

## Verification

N/A — no implementation changes for this domain.

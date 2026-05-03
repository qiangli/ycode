# Gap Analysis: AutoGen — Memory Management & Context Engineering

**Tool:** AutoGen (Python multi-agent framework)
**Source:** `priorart/autogen/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | AutoGen |
|------|-------|---------|
| Progressive compression | 4-layer cascade: observation masking → soft trim → microcompaction → LLM compaction | No multi-layer compression; either buffer or LLM-summarize |
| Microcompaction | Deterministic tool I/O clearing without LLM cost | Not present; context overflow escalates directly to LLM |
| Thinking block management | Preserve recent, clear older thinking blocks (Layer 1.5) | Ignores thinking blocks entirely |
| CJK-aware token estimation | ASCII 0.25 tokens/char, non-ASCII 1.3 tokens/char | Basic character counting only |
| Post-compaction recovery | FileTracker restores recently-edited files after summary (50K budget) | Loses file context after summarization |
| Prompt cache management | Fingerprinting, break detection, background warming (4.5min pings) | Application-level ChatCompletionCache only |
| Multi-backend search fusion | RRF + MMR re-ranking across Bleve + vector + keyword + entity | Single-backend similarity search only |
| Entity extraction & linking | EntityIndex.Link connecting entities to memories | Not present in core (experimental only) |
| Background memory extraction | Async extraction after assistant turns recognizing corrections/decisions | Not present; no auto-capture of user feedback |
| Memory TTL & sweep | Background sweeper removes expired memories (new from LangGraph analysis) | No TTL support |
| Batch operations | Store.Batch() with rollback (new from LangGraph analysis) | No batch atomicity |

## AutoGen Memory Features

AutoGen implements a modular memory architecture:
- **Core interface**: Abstract Memory protocol with query/add/update_context/clear
- **Vector backends**: ChromaDB (persistent+HTTP) and Redis (semantic+sequential modes)
- **Context windows**: Unbounded, Buffered (sliding window), HeadAndTail (keep first/last), TokenLimited
- **Task-centric memory** (experimental): MemoryController with task generalization and insight validation
- **Caching**: ChatCompletionCache wrapping any client with response fingerprinting

## Gaps Identified

No actionable gaps identified. ycode's memory and context engineering is significantly ahead of AutoGen across all studied dimensions. AutoGen's strengths (pluggable vector backends, task-centric learning) are either already covered by ycode's multi-backend fusion or are experimental/research-oriented.

## Verification

N/A — no implementation changes for this domain.

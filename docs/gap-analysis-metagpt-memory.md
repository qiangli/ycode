# Gap Analysis: MetaGPT — Memory Management & Context Engineering

**Tool:** MetaGPT (Python multi-agent framework)
**Source:** `priorart/metagpt/`
**Date:** 2026-05-03

## Where ycode Is Stronger

| Area | ycode | MetaGPT |
|------|-------|---------|
| Progressive compression | 4-layer cascade (masking → trim → microcompact → LLM) | BrainMemory sliding window + hierarchical summarization (2 layers) |
| CJK-aware token estimation | ASCII 0.25 tokens/char, non-ASCII 1.3 tokens/char | Basic word counting |
| Post-compaction recovery | FileTracker restores recently-edited files after summary | No file restoration |
| Prompt cache management | Fingerprinting, break detection, background warming | No cache management |
| Multi-backend search fusion | RRF + MMR re-ranking across Bleve + vector + keyword + entity | Single backend per query (FAISS or Chroma or BM25) |
| Entity extraction & linking | EntityIndex.Link connecting entities to memories | No entity linking |
| Background memory extraction | Async extraction recognizing corrections/decisions | No auto-extraction |
| Memory TTL & sweep | Background sweeper removes expired memories | Basic TTL constant but no sweep |

## MetaGPT Memory Features

MetaGPT has a three-tier memory system:
- **Short-Term Memory (STM)**: Message list indexed by role, content, action, position
- **Long-Term Memory (LTM)**: Transfers old STM messages to vector storage when capacity exceeds memory_k=200
- **BrainMemory**: Token-aware summarization with sliding windows, hierarchical compression, Redis-backed caching
- **RAG backends**: FAISS, Chroma, BM25, Elasticsearch with role-level integration
- **Message routing metadata**: cause_by, sent_from, send_to fields for multi-agent coordination

## Gaps Identified

No actionable gaps identified. MetaGPT's three-tier memory and RAG backends are well-designed but ycode already covers these capabilities through its multi-layer compression stack, multi-backend fusion search, and entity extraction. MetaGPT's message routing metadata (cause_by/sent_from) is interesting but functionally equivalent to ycode's context variable propagation in swarm handoffs.

## Verification

N/A — no implementation changes for this domain.

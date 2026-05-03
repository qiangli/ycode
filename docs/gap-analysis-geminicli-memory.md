# Gap Analysis: GeminiCLI — Memory Management & Context Engineering

**Tool:** Google Gemini CLI (TypeScript, Apache-2.0 license)
**Domain:** Memory Management & Context Engineering
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | GeminiCLI |
|------|-------|-----------|
| Memory types | 7 types × 4 scopes | 4-tier hierarchical (global/user-project/extension/project) |
| Retrieval | RRF fusion, 4 backends, MMR re-ranking | No semantic retrieval |
| Search backends | KV, SQL, Vector, Bleve FTS | No dedicated search |
| Persona modeling | KnowledgeMap, CommunicationStyle, BehaviorProfile | No user modeling |
| Entity extraction | NER, entity store, entity linking | No entity extraction |
| Token counting | CJK-aware with fast approximation | Heuristic (ASCII 0.33, non-ASCII 1.5) |
| Prompt caching | Fingerprinting, warming, break detection, completion cache | No explicit caching strategy |

## Where GeminiCLI Has Novel Approaches

| Area | GeminiCLI | ycode |
|------|-----------|-------|
| History representation | Episodic Context Graph (node-based with types/provenance) | Linear JSONL with rotation |
| Context processing | Event-driven processor pipeline (8 composable processors) | 5-layer defense (sequential) |
| Compression routing | Per-file batch routing (FULL/PARTIAL/SUMMARY/EXCLUDED) | Per-tool-output distillation |
| Provenance tracking | Pristine + active graph duality, abstractsIds | No explicit provenance |

## Gaps Identified

| ID | Feature | GeminiCLI Implementation | ycode Status | Priority | Effort |
|----|---------|-------------------------|--------------|----------|--------|
| M1 | Episodic Context Graph | Graph nodes with types (USER_PROMPT, TOOL_EXECUTION, SNAPSHOT, etc.), provenance tracking | ycode uses linear JSONL messages | Low | High |
| M2 | Per-file compression routing | Batch LLM query classifies each file as FULL/PARTIAL/SUMMARY/EXCLUDED | ycode has distillation thresholds but not per-file semantic routing | Low | High |
| M3 | Dual-model compression verification | Second LLM pass self-corrects omissions in summaries | ycode uses single-pass compaction | Low | Medium |

## Implementation Plan

**No actionable gaps identified.** GeminiCLI's Episodic Context Graph is architecturally novel but would require a fundamental redesign of ycode's message handling. ycode's 5-layer defense system and multi-backend retrieval already provide superior practical outcomes.

---

## Summary

GeminiCLI introduces genuinely novel concepts (ECG, processor pipeline, per-file routing) that represent alternative architectural approaches rather than missing capabilities. ycode's linear but multi-layered approach is simpler and equally effective for the CLI use case.

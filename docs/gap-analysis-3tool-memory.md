# Gap Analysis: Memory & Context Engineering — paperclip + openclaw + opencode

Combined analysis across three agentic tools vs ycode.

## Where ycode Is Stronger

| Area | ycode Advantage | Compared To |
|------|----------------|-------------|
| Prompt Caching | Static/dynamic boundary + differential diffing + cache warmer (4.5min keep-alive) + completion cache | Openclaw has stable prefix caching; opencode has session prefetch; paperclip has planned but unshipped |
| Observation Masking | Three-layer pruning (soft trim → hard clear → mask) with configurable thresholds | Openclaw has two-layer (strip + mark); opencode has tool output stripping only |
| Adaptive Recall | RecallFlow with confidence-based deepening + LLM sub-queries + RRF fusion + MMR re-ranking | Openclaw has hybrid MMR; opencode has no semantic search; paperclip has planned but unshipped |
| Multi-Scope Memory | 4 scopes (global/project/team/user) with hierarchical paths, 7 types, TTL expiration | Openclaw has file-based root memory; opencode has session-only; paperclip has planned architecture |
| Storage Architecture | Progressive init (KV instant → SQL background → vector lazy), pure Go, zero CGO | All three use single-stage init |
| Token Estimation | CJK-aware (0.25 ASCII, 1.3 CJK) multi-byte token math | Openclaw has CJK-aware too; opencode uses 4 chars/token flat |
| Memory Consolidation | Dreamer with 30-min intervals, LLM-backed merge decisions, stale removal | Openclaw has 3-phase dreaming (light/deep/REM) but ycode's is integrated with recall scoring |
| Prompt Sections | 16 sections with diagnostic injection, persona, repo map, built-in skills | Openclaw has 7 context files; opencode has provider-aware templates |
| Entity Linking | NER-based entity extraction, entity index, entity-scoped search (partial) | None of the three have entity linking |

## Gaps Identified

| ID | Feature | Source Tool | ycode Status | Priority | Effort |
|----|---------|-------------|-------------|----------|--------|
| M1 | Identifier preservation during compaction | openclaw | Missing — compaction doesn't protect UUIDs/hashes/paths | High | Small |
| M2 | Transcript repair for orphaned tool pairs | openclaw | Partial — analyzeMessageStructure detects but doesn't repair | Medium | Small |
| M3 | Anchored compaction summary (merge into prior) | opencode | Missing — compaction creates fresh summary each time | Medium | Medium |
| M4 | Multi-phase dreaming (light/deep/REM) | openclaw | Partial — single-tier Dreamer exists | Low | Medium |
| M5 | Adapter-native context management tiers | paperclip | Missing — no per-provider compaction policy | Low | Small |
| M6 | Continuation summary as durable document | paperclip | Missing — no structured per-task context carry-forward | Low | Medium |

## Implementation Plan

### Phase 1: M1 — Identifier Preservation During Compaction (High)

**Rationale:** When the context is compacted/summarized, file paths, commit hashes, UUIDs, and other identifiers get lost. This is critical for autonomous agents that need to reference specific files or commits across compaction boundaries.

**Design:**
- New file: `internal/runtime/session/idpreserve.go`
- Extract identifiers from messages before compaction using regex patterns
- Inject preservation instruction into compaction prompt
- Validate identifiers survive in compacted output
- Configurable policy: strict (inject all), custom (filtered), off

**Patterns to preserve:**
- File paths: `/path/to/file.go`, `./relative/path`
- Git hashes: 7-40 char hex
- UUIDs: standard 8-4-4-4-12 format
- URLs: `https://...`
- Go package paths: `github.com/...`

### Phase 2: M2 — Transcript Repair (Medium)

**Rationale:** After compaction or message pruning, tool_use blocks can become orphaned (missing their tool_result) or vice versa. This confuses LLMs. OpenClaw's transcript repair auto-fixes these.

**Design:**
- New file: `internal/runtime/session/transcript_repair.go`
- Scan messages for unpaired tool_use/tool_result blocks
- For orphaned tool_use: inject synthetic "[result pruned during compaction]" result
- For orphaned tool_result: remove or annotate as orphaned
- Run automatically after compaction

### Phase 3: M3 — Anchored Compaction Summary (Medium)

**Deferred to next cycle** — requires deeper refactor of compaction flow.

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| M3 | Anchored compaction summary | Requires refactoring compaction to be incremental; current fresh approach works |
| M4 | Multi-phase dreaming | Single-tier Dreamer adequate; phases add complexity without clear ROI yet |
| M5 | Adapter context tiers | ycode currently uses single provider; multi-provider relevance low |
| M6 | Continuation summary | Episodic memory serves similar purpose; structured carry-forward deferred |

## Verification

- `go test -short -race ./internal/runtime/session/...`
- `make build` must pass

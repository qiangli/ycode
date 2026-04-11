# Memory System: Implementation Checklist

Cross-reference of `docs/plan-memory.md` against actual code. Each item verified by reading the implementation, not just checking file existence.

## 1. Context Defense (3-Layer Stack + Layer 0)

- [x] **Layer 0: Observation masking** — `MaskOldObservations()` in `session/pruning.go` replaces old tool results with `<MASKED>` outside attention window (10 messages)
- [x] **Layer 1: Context pruning** — `PruneMessages()` in `session/pruning.go` with soft trim (60% ratio, head+tail with `[... N characters omitted ...]`) and hard clear (80% ratio, placeholder replacement)
- [x] **Layer 2: Session compaction** — `buildIntentSummary()` in `session/compact.go` produces structured `<intent_summary>` with 8 categories (Scope, Primary Goal, Verified Facts, Working Set, Active Blockers, Decision Log, Key Files, Tools Used, Pending Work)
- [x] **Layer 3: Emergency memory flush** — `emergencyFlush()` in `conversation/runtime.go` creates minimal continuation with summary + last user message
- [x] **Post-compaction context refresh** — `PostCompactionRefresh()` in `prompt/refresh.go` re-injects critical CLAUDE.md sections (Build & Test, Key Design Decisions, Dependencies) within 3000-char budget
- [x] **Model-aware context budgets** — `ContextBudgetForModel()` in `session/budget.go` dynamically scales thresholds based on model context window (proportional reservation: 20% reserved, 50% compaction for large windows)
- [x] **Proactive auto-compaction** — `TurnWithRecovery()` in `conversation/runtime.go` checks `health.NeedsCompactionNow()` BEFORE the API call, not just reactively
- [x] **Context health monitoring** — `CheckContextHealth()` in `session/pruning.go` reports 4 levels: Healthy (<60%), Warning (60-80%), Critical (80-100%), Overflow (>100%)

## 2. Context Enrichment

- [x] **Differential context injection** — `ContextBaseline` in `prompt/baseline.go` with SHA-256 section hashing and `Diff()` returning Changed/Unchanged sections for non-caching providers
- [x] **Provider capability detection** — `DetectCapabilities()` in `api/capabilities.go` with `CachingSupported` field; static lookup for Anthropic/OpenAI models
- [x] **JIT subdirectory context loading** — `JITDiscovery.OnToolAccess()` in `prompt/jit.go` discovers instruction files from accessed directory up to project root
- [x] **`#import` directive** — `ResolveImports()` in `prompt/import.go` with circular detection via visited map and `MaxImportDepth = 3`
- [x] **Startup prewarming** — `Prewarm()` in `prompt/prewarm.go` uses `sync.WaitGroup` to discover instruction files and load memories concurrently
- [x] **Active topic tracking** — `TopicTracker` in `prompt/topic.go` with extraction, `[Active Topic: ...]` injection, and staleness after `TopicMaxTurns = 20`

## 3. Compaction Quality

- [x] **Structured intent summary** — `buildIntentSummary()` in `session/compact.go` with dedicated helpers: `inferPrimaryGoal`, `extractVerifiedFacts`, `extractWorkingSet`, `extractActiveBlockers`, `extractDecisionLog`
- [x] **Ghost snapshots** — `SaveGhostSnapshot()` in `session/ghost.go` serializes pre-compaction state (MessageCount, EstimatedTokens, Summary, CompactedIDs, ActiveTopic) to JSON
- [x] **Cumulative state snapshots** — `UpdateStateSnapshot()` in `session/state_snapshot.go` updates (not appends) on each compaction; tracks goal, steps, files, environment, compaction count
- [x] **Summary compression** — `CompressSummary()` in `session/compression.go` enforces `MaxSummaryChars = 1200`, `MaxSummaryLines = 24` with priority tiers P0-P3

## 4. History Integrity

- [x] **History normalization** — `NormalizeHistory()` in `session/normalize.go` enforces call-output pairing; synthesizes missing results as aborted, removes orphan results
- [x] **Tool output distillation** — `DistillToolOutput()` in `session/distill.go` with two-stage truncation (head 20 + tail 10 lines) and disk-backed full output via `saveFullOutput()`
- [x] **Per-file content routing** — `RouteContent()` in `session/routing.go` classifies as `RouteFull`, `RoutePartial`, `RouteSummary`, `RouteExcluded` with heuristic-based classification

## 5. Persistent Memory

- [x] **Hierarchical memory scopes** — `NewManagerWithGlobal()` in `memory/memory.go` with dual-store (global + project); project-scoped memories get 1.1x score boost in `Recall()`
- [x] **Typed staleness thresholds** — `StalenessThresholds` map in `memory/age.go`: project=30d, reference=90d, user=180d, feedback=365d
- [x] **Temporal decay scoring** — `DecayScore()` in `memory/age.go` with logarithmic formula `1/(1 + days/30)` after 7-day grace period
- [x] **Background dreaming** — `Dreamer` in `memory/dream.go` runs on 30-min interval; `consolidate()` removes stale entries, `mergeSimilar()` merges duplicate project memories
- [x] **MEMORY.md index** — `AddEntry()`/`RemoveEntry()` in `memory/index.go` with `MaxIndexLines = 200` cap
- [x] **Multi-backend search** — Vector (`vectorindex.go`), Bleve BM25 (`bleveindex.go`), keyword fallback (`search.go`); `Recall()` in `memory.go` orchestrates all three in sequence

## 6. Safety & Reliability

- [x] **Loop/stuck detection** — `LoopDetector` in `conversation/loop_detector.go` with `isSimilar()` (0.85 threshold), soft warning at 3 repeats, hard termination at 5
- [x] **Auto-checkpointing** — `AutoCheckpointer.OnCompaction()` in `scratchpad/auto.go` saves checkpoint with session ID, summary, and compacted count on compaction events

## 7. Advanced Compaction

- [x] **LLM-based summarization** — `LLMSummarizer.Summarize()` in `session/llm_summary.go`; `CompactWithLLM()` in `session/compact.go` uses LLM with heuristic fallback; enabled via `llmSummarizationEnabled` config flag
- [x] **Agent-requested condensation** — `compact_context` tool in `tools/compact_context.go`; `CompactNow()` in `conversation/runtime.go` triggers immediate compaction; wired via `App.CompactContext()` callback

## Summary

**30/30 features implemented and verified.** All items from the consolidated plan are complete.

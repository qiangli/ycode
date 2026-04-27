# hermes-agent Feature Integration

Summary of features incorporated from [hermes-agent](../priorart/hermes-agent/) into ycode, plus the self-training pipeline inspired by hermes' Atropos environments and [nanochat](../../nanochat/).

## Overview

26 items implemented across 6 phases. All code compiles, passes `go vet`, and all tests pass with `-race`.

**New files created**: 56
**Existing files modified**: 20+
**New test files**: 20+

---

## Phase 1: Context Intelligence and Resilience

### 1.1 Structured Context Compression
Head/tail protection in compaction: first 2 user turns are preserved verbatim (head), last N messages preserved (tail), only the middle is summarized. Summary now includes structured sections: Resolved Questions, Active Task, Remaining Work.

**Files**: `internal/runtime/session/compact.go` (modified)

### 1.2 Enhanced Error Classification with Smart Recovery
`FailoverReason` enum (13 categories: Auth, Billing, RateLimit, Overloaded, ServerError, Timeout, ContextOverflow, PayloadTooLarge, ModelNotFound, PolicyBlocked, FormatError, etc.) with `ClassifyError()` and per-reason `RecoveryAction` (Retry, RotateKey, FallbackModel, CompressContext, Abort). Wired into `doWithRetry` and `FallbackProvider.Send`.

**Files**: `internal/api/errors.go`, `internal/api/retry.go`, `internal/api/fallback.go` (all modified)

### 1.3 Prompt Injection Detection
Scans context files for 4 pattern categories before including them in prompts: known injection phrases (High), invisible unicode (High), HTML/script tags (Medium), base64 blocks (Low). Logs warnings and prepends a security note when findings are detected.

**Files**: `internal/runtime/security/injection.go` (new), `internal/runtime/prompt/sections.go` (wired)

### 1.4 Iteration Budget with Grace Call
`IterationBudget` replaces the flat `maxToolIterations` constant. After the normal budget is exhausted, one grace call is allowed with a system message instructing the LLM to wrap up.

**Files**: `internal/runtime/conversation/budget.go` (new), `internal/service/local.go` (wired)

---

## Phase 2: Platform and Identity

### 2.1 Personality/Identity System (SOUL.md)
`LoadSOUL()` reads a user-defined identity from `SOUL.md` (project root or `~/.ycode/`). 6 builtin personality presets: default, pirate, shakespeare, stern, teacher, kawaii. Personality section injected into system prompt between intro and system sections. Config fields: `Personality`, `SOULFile`.

**Files**: `internal/runtime/prompt/personality.go` (new), `internal/runtime/prompt/builder.go` (wired), `internal/runtime/prompt/context.go` (Personality field), `internal/runtime/config/config.go` (config fields)

### 2.2 Expanded Messaging Adapters
Scaffold adapters for Slack (Socket Mode), Matrix (CS API), and Email (IMAP/SMTP) implementing the existing `Channel` interface. Send methods return not-implemented — ready for protocol implementation.

**Files**: `internal/chat/adapters/slack.go`, `matrix.go`, `email.go` (new), `internal/chat/channel/channel.go` (constants)

### 2.3 Platform-Specific Prompt Hints
Per-platform formatting guidance injected into the system prompt: Telegram (MarkdownV2), Discord (Discord markdown), Slack (mrkdwn), Matrix (standard Markdown), Email (plain text/HTML), WhatsApp (no markdown), Web (full Markdown).

**Files**: `internal/runtime/prompt/platform_hints.go` (new), `internal/runtime/prompt/sections.go` (constants)

### 2.4 Cron Delivery Routing
`DeliveryTarget` struct (ChannelType, ChannelID) added to schedule entries. Cron job results can be routed to specific messaging platforms.

**Files**: `internal/runtime/loop/delivery.go` (new), `internal/runtime/loop/scheduler.go` (field added)

---

## Phase 3: Tool System and Memory

### 3.1 Toolset Composition
`ToolsetRegistry` with 9 builtin toolsets (core, web, git, memory, file_extended, search, research, full_stack, read_only). Toolsets support recursive composition — "research" includes "web" + "search". `NewFilteredRegistryFromToolsets` constructor. Config field for user-defined toolsets.

**Files**: `internal/tools/toolset.go` (new), `internal/tools/filtered.go` (constructor), `internal/runtime/config/config.go` (field)

### 3.2 Memory Provider Plugin Interface
`MemoryProvider` interface with core operations (Save/Load/List/Delete/Search) and lifecycle hooks (OnTurnStart, OnPreCompress, OnMemoryWrite, OnDelegation, OnSessionEnd). `FileProvider` wraps the existing Store as the default. Manager calls `OnMemoryWrite` after each save.

**Files**: `internal/runtime/memory/provider.go` (new), `internal/runtime/memory/memory.go` (wired)

### 3.3 Subagent Blocked Tools
`DefaultSubagentBlocklist` prevents subagents from: recursive delegation (Agent, Handoff), user interaction (AskUserQuestion), memory corruption (MemorySave, MemoryForget), scheduling (CronCreate, CronDelete), and remote triggers. Applied in spawner for all agent types.

**Files**: `internal/tools/allowlists.go` (blocklist + ApplyBlocklist), `internal/runtime/conversation/spawner.go` (wired)

---

## Phase 4: User Experience

### 4.1 LLM Session Title Generation
`FormatTitlePrompt()` and `GenerateLLMTitle()` produce a prompt for a cheap model to generate concise 3-6 word session titles, replacing simple text truncation.

**Files**: `internal/runtime/session/session.go` (functions added)

### 4.2 Per-Model Cost Tracking
`PricingTable` with 12 model entries (Claude Opus/Sonnet/Haiku 4, GPT-4o/4o-mini, o3/o3-mini/o4-mini, Gemini 2.5 Pro/Flash). `LookupPricing()` uses longest-prefix matching. Tracker's `AddWithModel()` uses model-specific pricing instead of hardcoded Sonnet rates.

**Files**: `internal/runtime/usage/pricing.go` (new), `internal/runtime/usage/tracker.go` (updated)

### 4.3 Cross-Session Search Command
`/search <query>` command using the existing `session.Search` infrastructure. Also added `IndexSearchResult` type and `Search()` method on `SearchIndexer` for Bleve-backed full-text search.

**Files**: `internal/commands/cmd_search.go` (new), `internal/runtime/session/searchindex.go` (method added)

### 4.4 Batch Runner
`BatchPrompt`/`BatchResult`/`BatchStats` types with JSONL I/O. `Checkpoint` for resume-from-failure. `RunnerConfig` with concurrency and retry settings. Cobra subcommand: `ycode batch run --input prompts.jsonl`.

**Files**: `internal/runtime/batch/runner.go`, `checkpoint.go` (new), `cmd/ycode/batch.go` (cobra cmd)

### 4.5 Insights/Analytics Command
`InsightsReport` with session counts, token totals, cost estimates, tool usage stats, sessions-per-day breakdown. `FormatInsights()` renders a formatted table.

**Files**: `internal/commands/cmd_insights.go` (new)

---

## Phase 5: Self-Training (End-to-End RL Pipeline)

### 5.1 Training Task Framework
`Task` interface with `GetExample`/`Evaluate`/`Len`. 5 concrete implementations:
- **GSM8K** — math word problems with number-extraction evaluator
- **HumanEval** — code generation with partial-credit evaluation
- **SWE** — bug-fix tasks with keyword matching
- **Terminal** — file creation tasks with content verification
- **WebResearch** — multi-step research with tool metadata

`Mixture` combines tasks with weighted sampling.

**Files**: `internal/training/task/task.go`, `gsm8k.go`, `humaneval.go`, `swe.go`, `terminal.go`, `web_research.go`, `mixture.go`

### 5.2 Reward Engine
`RewardFunc` interface with `Score()`. Implementations:
- **BinaryReward** — pass/fail from a check function
- **MultiSignalReward** — weighted combination of named signals
- **LLMJudgeReward** — LLM-as-judge scoring with configurable JudgeFunc callback
- **ToolContext** — sandbox access for reward verification (same environment the agent used)

Helper signals: `ToolUsageSignal` (did the agent use specific tools?) and `EfficiencySignal` (penalizes excessive tool calls).

**Files**: `internal/training/reward/reward.go`, `binary.go`, `multisignal.go`, `llm_judge.go`, `tool_context.go`

### 5.3 Trajectory Collection
`ScoredTrajectory` with messages, score, turns, tool errors, duration. `SaveTrajectories`/`LoadTrajectories` for JSONL persistence. `ComputeStats` for aggregate metrics (avg score, pass rate, error count). `ResultBudget` for per-tool output size limits during rollouts.

**Files**: `internal/training/rollout/trajectory.go`, `collector.go`, `budget.go`

### 5.4 Tool Call Parsers
`Parser` interface with `Parse()` returning content + tool calls. Two implementations:
- **HermesParser** — `<tool_call>{"name": ..., "arguments": ...}</tool_call>` XML format
- **JSONParser** — JSON array format (Llama, Qwen style)

Global registry with `Get(name)` and `Register(parser)`.

**Files**: `internal/training/parsers/parser.go`, `hermes.go`, `json_parser.go`, `registry.go`

### 5.5 RL Trainer (GRPO)
Go orchestrator (`GRPOTrainer`) that spawns a Python subprocess for GPU training. Sends trajectories via stdin JSON, reads `StepResult` (loss, grad norm, step time) from stdout. JSONL bridge protocol with `BridgeWriter`/`BridgeReader` for IPC.

**Files**: `internal/training/trainer/grpo.go`, `bridge.go`

### 5.6 Training CLI Commands
Config types: `TrainConfig`, `CollectConfig`, `EvalConfig`. Cobra subcommands:
- `ycode train rl --task gsm8k --model local:qwen3-4b --steps 500`
- `ycode train collect --task terminal --output trajectories.jsonl`
- `ycode train eval --task gsm8k --model local:qwen3-4b`

**Files**: `internal/training/cmd.go`, `cmd/ycode/train.go`

### 5.7 Closed Learning Loop
`SelfImproveLoop` with callback-based collect/train/evaluate/swap cycle. Stops when improvement falls below threshold. Filters to high-reward trajectories only. `CurriculumState` with 4 difficulty levels (Easy/Medium/Hard/Extreme) and promotion/demotion based on pass rate thresholds. `CheckpointManager` with save/best/latest/rollback support.

**Files**: `internal/training/loop/selfimprove.go`, `curriculum.go`, `checkpoint.go`

---

## Phase 6: Operational Hardening

### 6.1 Atomic File Operations
`AtomicWriteFile` (write-to-temp + fsync + rename) prevents corruption from crashes. Applied to: memory store, memory index, session state snapshots, ghost snapshots, distill cache, model overrides, child session state.

**Files**: `internal/runtime/fileops/atomic.go` (new), `internal/runtime/memory/store.go`, `index.go`, `internal/runtime/session/state_snapshot.go`, `ghost.go`, `distill.go`, `model_override.go`, `child.go` (all updated)

### 6.2 Skills as User Messages for Cache Preservation
`SkillInjectionMode` with `SkillInjectSystem` (default) and `SkillInjectUser` (for Anthropic prompt caching). `RecommendedSkillInjection()` selects mode based on provider caching support. `FormatSkillAsUserMessage()` wraps skill content with a system note. `BuildSkillInjection()` combines both.

**Files**: `internal/runtime/prompt/skill_injection.go` (new)

### 6.3 Approval Queue Routing for Platforms
`ApprovalRouter` with mutex-safe pending request map, configurable timeout, and `RequestApproval`/`Respond` methods. Hub stub `RequestApproval()` for platform routing (default-deny until platform adapters implement the protocol).

**Files**: `internal/runtime/permission/approval_routing.go` (new), `internal/chat/hub.go` (stub)

---

## Remaining Work

### Python GRPO Training Worker (`scripts/training/grpo_worker.py`)

The Go orchestrator (`internal/training/trainer/grpo.go`) and IPC bridge (`bridge.go`) are complete. The missing piece is the Python counterpart that receives trajectories via stdin, runs the actual GPU training, and returns step results.

**What it needs to do:**
1. Read JSONL trajectories from stdin
2. Load a model checkpoint (Ollama GGUF or HuggingFace format)
3. Run simplified GRPO: compute per-token advantages from group rewards, apply policy gradient
4. Write `StepResult` JSON to stdout (loss, grad_norm, step_time)
5. Save updated checkpoint to disk

**Reference implementations:**
- `../nanochat/scripts/chat_rl.py` — simplified GRPO with DAPO-style advantages, Muon+AdamW optimizer
- `priorart/hermes-agent/environments/hermes_base_env.py` — Atropos two-phase data collection pattern

**Suggested approach:** Extract the core GRPO loop from nanochat's `chat_rl.py`, wrap it in a stdin/stdout JSONL interface matching the `TrainRequest`/`TrainResponse` protocol in `trainer/bridge.go`. Use PyTorch for GPU training, tiktoken for tokenization.

**Dependencies:** PyTorch, tiktoken, the model architecture from nanochat (or use HuggingFace transformers for broader model support).

This is intentionally deferred because it's a Python file requiring GPU infrastructure and the nanochat model architecture — it doesn't fit the Go compilation workflow and should be developed and tested independently against the bridge protocol.

---

## Features Intentionally Excluded

- **Skin/Theme Engine** — cosmetic, low impact for agent capability
- **Voice Memo Transcription** — niche, platform-specific
- **MEDIA:/ delivery** — can be added when platform adapters mature
- **RL Training Environments (Atropos direct integration)** — replaced by Go-native training pipeline (Phase 5) which draws from the same patterns but doesn't depend on Atropos Python framework

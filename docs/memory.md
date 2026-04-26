# Memory

## Overview

This document consolidates the architectural details, implementation specifics, and theoretical research for the agentic memory model used in the Go-based ycode stack. Updated with SOTA developments through April 2026.

ycode's differentiator is **file-first, service-enhanced memory with Memos as the wiki manager**. All memory artifacts — session logs, compaction summaries, persistent memories, traces, metric snapshots — are written as human-readable markdown/JSONL files on disk, always, regardless of whether services are running. [Memos](https://usememos.com/) organizes these artifacts as tagged, searchable, linkable wiki pages. [Prometheus](https://prometheus.io/) provides real-time alerting when services are running; periodic snapshots to disk when not.

## File-First, Service-Enhanced Architecture

### Design Principles

1. **Disk is the source of truth** — all logs, traces, metric snapshots, and memory artifacts are markdown/JSONL files on the filesystem
2. **Services are additive, not required** — `ycode` works fully offline; `ycode serve` adds real-time indexing, search, alerts, and dashboards
3. **Memos as wiki manager** — organizes memory artifacts as tagged, searchable, linkable wiki pages with cross-references
4. **Catch-up indexing** — on `ycode serve` startup, scan for new/updated files written while services were down; index into vector store, FTS, and Memos API
5. **Periodic snapshots** — real-time metrics (Prometheus) and traces (OTEL) get periodic disk snapshots so nothing is lost on service restart
6. **Auditability** — every agent decision, memory mutation, and policy evaluation is traceable through markdown files with bi-directional links

### Two Operating Modes

| Aspect | CLI-only (`ycode`) | Full stack (`ycode serve`) |
|--------|-------------------|---------------------------|
| Memory persistence | Markdown files on disk | Markdown files + Memos API indexing |
| Session logs | JSONL files | JSONL + indexed for search |
| Traces | OTEL trace files on disk | OTEL files + live Jaeger |
| Metrics | Periodic snapshots to disk | Live Prometheus + snapshots |
| Search | Keyword search on files | Vector (chromem-go) + FTS (Bleve) + Memos tag search |
| Alerts | None | Prometheus alerting rules |
| Wiki | Files browsable in editor/git | Memos web UI + API |

### Catch-Up Indexing Flow

```
ycode serve startup
  → scan memory/ directories for new/modified files (mtime-based)
  → parse YAML frontmatter + markdown content
  → upsert into chromem-go vector store (embeddings)
  → upsert into Bleve FTS index
  → sync to Memos API (create/update memos with tags)
  → index JSONL session files written since last serve
  → import OTEL trace files into Jaeger
```

## ycode 5-Layer Implementation

| Layer | Name | Disk Artifact | Service Enhancement | Key Files |
|-------|------|--------------|--------------------|-----------| 
| **L1** Working | Context window | Current session in memory | — | `session/session.go` |
| **L2** Short-term | JSONL sessions | `sessions/{id}/messages.jsonl` (256KB rotation, 3 backups) | Indexed on serve startup | `session/session.go` |
| **L3** Long-term | Compaction | Intent summary in session JSONL | Searchable via Memos | `session/compact.go` |
| **L4** Contextual | Ancestry | CLAUDE.md/AGENTS.md files in repo tree | — | `prompt/discovery.go` |
| **L5** Persistent | File-based memory | `memory/{name}.md` with YAML frontmatter | Vector + FTS indexed on serve | `memory/types.go`, `memory/memory.go` |

### Memory Types and Scopes

Four memory types (`memory/types.go`): **user** (role, preferences, knowledge), **feedback** (corrections and confirmed approaches), **project** (ongoing work, goals, decisions), **reference** (pointers to external systems).

Two scopes: **global** (`~/.agents/ycode/memory/` — shared across all projects) and **project** (`~/.agents/ycode/projects/{hash}/memory/` — project-specific). Defaults to project scope.

### Search Backends

Three-tier search with graceful degradation:

| Backend | Technology | Offline | With Serve | Use Case |
|---------|-----------|---------|------------|----------|
| Keyword | Go string matching | Yes | Yes | Fallback, always available |
| Full-text | Bleve v2 (BM25, fuzzy, stemming) | Indexed on disk | Live index | Phrase queries, fuzzy matching |
| Vector | chromem-go (cosine similarity, GZIP GOBs) | Indexed on disk | Live embeddings | Semantic similarity search |

Files: `storage/vector/vector.go`, `storage/search/search.go`, `memory/vectorindex.go`

### Temporal Decay and Staleness

Logarithmic temporal decay after a 7-day grace period: `score * 1/(1 + days/30)`. Type-stratified staleness thresholds: project facts (30d), reference (90d), user preferences (180d), feedback (365d). Relevance scoring weights: name match (3x), description (2x), content (1x).

### Background Dreaming

`Dreamer` (`memory/dream.go`): 30-minute background consolidation cycles — stale removal, duplicate merging, temporal decay enforcement. Writes reflection memos to disk; consolidated by Memos service on next serve cycle.

### Memos Tools

When `ycode serve` is running, the agent has access to: **MemosStore**, **MemosSearch**, **MemosList**, **MemosDelete** (`tools/memos.go`). These tools operate via the Memos REST API client (`memos/client.go`) and are exempt from observation masking to preserve memory operations in context.

## Context Defense

Three-layer defense with constants from `session/pruning.go` and `session/compact.go`:

### Layer 0: Observation Masking

Replaces old tool result content with `<MASKED>`. Two modes:

- **Count-based**: window=10 (caching providers) or 6 (non-caching). Recent N tool results remain unmasked.
- **Budget-based**: 50K token protection budget for newest outputs; only masks if prunable tokens > 30K (batch threshold). Exempt tools: AskUserQuestion, MemosStore, MemosSearch, MemosList, EnterPlanMode, ExitPlanMode, Skill.

### Layer 1: Context Pruning

- **Soft trim** at 60% of CompactionThreshold (60K tokens): old tool results truncated to 600 chars (15% head / 85% tail — headers preserved, error messages/results kept).
- **Hard clear** at 80% (80K tokens): old tool results replaced with `[Tool output pruned to save context. Re-run the tool if needed.]`
- Last 6 messages (`RecentMessagesProtected`) are never pruned.

### Layer 2: Session Compaction

Triggered at 100K tokens (`CompactionThreshold`). Produces a structured intent summary with categories:

- **Primary Goal**: inferred from recent user requests
- **Verified Facts**: successful tool outcomes (tests passing, builds succeeded, files modified)
- **Working Set**: files actively written/edited
- **Active Blockers**: error outputs from recent tool executions
- **Decision Log**: explicit choices made by the assistant
- **Key Files**, **Tools Used**, **Pending Work**

LLM-based summarization (`CompactWithLLM`) when available; heuristic `buildIntentSummary` as fallback. Preserves last 4 messages verbatim. Never splits tool-use/tool-result pairs at the compaction boundary. Summary budget enforcement via recursive head/tail splitting (67% tail, 33% head).

### Layer 3: Emergency Flush

Minimal continuation with summary + last user message. Triggered when compaction + retry still exceeds provider limits.

### Agent-Requested Compaction

The `compact_context` tool (`tools/compact_context.go`) lets the agent trigger compaction on demand when it senses context bloat.

### Context Health Monitoring

`CheckContextHealth` (`session/pruning.go`) reports four levels: **healthy** (<60%), **warning** (60-80%), **critical** (80-100%), **overflow** (>100%).

### Token Estimation

CJK-aware: ASCII chars (0-127) ≈ 0.25 tokens/char, non-ASCII ≈ 1.3 tokens/char. For large strings (>100K chars), falls back to `len/4` for performance.

### Tool Output Distillation

Two-tier thresholds (`session/distill.go`):

| Threshold | Normal (caching) | Aggressive (non-caching) |
|-----------|-----------------|-------------------------|
| Max inline chars | 2,000 | 1,000 |
| Max inline bytes | 50 KB | 25 KB |
| Max inline lines | 2,000 | 1,000 |
| Head / tail lines | 20 / 10 | 12 / 6 |

Full output saved to disk in `tool-output/` directory with inline reference.

## Prompt Optimization

- **Differential context injection** (`prompt/baseline.go`): Per-section SHA-256 baseline tracking. For non-caching providers, only changed sections are re-sent; unchanged sections replaced with `[N section(s) unchanged]`.
- **Cache warming** (`api/cache_warmer.go`): Background pings every 4.5 minutes (ahead of Anthropic's 5-min TTL) using minimal request with same system prompt + tools.
- **Prompt fingerprinting** (`api/prompt_cache.go`): SHA-256 hash of model + system + tools + messages. Detects cache breaks when cache read tokens drop >2,000 unexpectedly. 5-minute TTL.
- **Completion cache** (`api/completion_cache.go`): 30-second TTL, disk-backed. Skips LLM entirely for identical requests (retries, error recovery).
- **Startup prewarming** (`prompt/prewarm.go`): Concurrent discovery of instruction files + memory loading.
- **JIT context loading** (`prompt/jit.go`): Discover instruction files when tools access new paths; content-hash deduplication prevents duplicate injection.
- **`#import` directive** (`prompt/import.go`): Instruction files support `#import <path>` with circular-reference detection, max depth 3.
- **Active topic tracking** (`prompt/topic.go`): Extract task from user messages, inject into system prompt, clear after 20 turns.
- **Post-compaction refresh** (`prompt/refresh.go`): Re-inject critical CLAUDE.md sections after compaction.

## 9-Layer Agentic Memory Reference Model

| Layer | Type | Purpose | Go Stack Implementation | Traceability ([Memos](https://usememos.com/)) | LLM Perspective | I/O Category | Persistence |
|:------|:-----|:--------|:------------------------|:-----------------------------------------------|:----------------|:-------------|:------------|
| **L1** | **Working** | Immediate context and active reasoning: the "RAM" of the agent. Holds current task goals, recent conversation turns, and Plan/Act/Verify loop state. | Context window + JSONL sessions (`session/session.go`). Managed via local Go variables during runtime. | **Snapshot:** Session summaries written to disk as markdown with `#working` tag; indexed by Memos service when available. | The "Conversation": literal text of the last few messages and active thread context. | Input: Current message + immediate session history. | Volatile (Current Session) |
| **L2** | **Episodic** | Auto-biographical event logs: chronological records of specific past actions and outcomes. Answers "What did I do last time?" to avoid repeating errors. | JSONL append (256KB rotation, 3 backups) + [OTEL](https://opentelemetry.io/) trace files written to disk. Trace IDs map to specific historical agent "episodes." | **Log Cards:** Episode log cards written to disk as markdown; OTEL traces indexed on `ycode serve` startup. | The "Flashback": summarized snippet or re-hydrated log of a specific past event injected into the prompt. | Reference: Retrievable context for the current prompt. | Long-term (Searchable) |
| **L3** | **Semantic** | The internal encyclopedia: general facts, technical documentation, and concept relationships independent of specific events. | [chromem-go](https://github.com/philippgille/chromem-go) vector store (`storage/vector/`) + [Bleve v2](https://blevesearch.com/) FTS (`storage/search/`). Utilizes RAG (Retrieval-Augmented Generation). | **Knowledge Memos:** Markdown files with `#docs` tags on disk; vector/FTS indexes rebuilt on `ycode serve` startup. | The "Knowledge": static facts, code snippets, or documentation provided as reference text. | Reference: Static data injected via RAG. | Permanent (Knowledge Base) |
| **L4** | **Procedural** | The internal instruction manual: "How-to" skills and execution logic. Governs the steps for debugging, deploying, or interacting with system APIs. | Go Functions and [MCP](https://modelcontextprotocol.io) Tool Definitions (`internal/tools/`). Hard-coded logic gates that the agent triggers to perform physical actions. | **Tool Heatmap:** Tool-call patterns and optimization notes written as tagged markdown files. | The "Toolkit": JSON schema defining available functions and tools the LLM is permitted to call. | Output: Tool Calls (JSON) sent back to your Go binary. | Fixed (Logic/Skills) |
| **L5** | **Reflective** | Self-optimization and metacognition: high-level abstractions and "lessons learned." The agent analyzes its own episodic patterns to refine future strategies. | Background Dreamer: 30-min stale removal + duplicate merging (`memory/dream.go`). Distills L2 (Episodic) data into high-level rules. | **Lesson Learned:** Reflection memos written to disk after failures; consolidated on next serve cycle. | The "Strategy": distilled lessons, optimized plans, and corrected behaviors based on self-reflection. | Feedback Loop: Meta-analysis of past performance. | Evolving (Continuous) |
| **L6** | **Observability** | Self-awareness and efficiency: monitoring the agent's own health, read/write costs, and latency. Prevents context window bloat and manages token budgets. | **[Prometheus](https://prometheus.io/)** for live metrics and alerts when `ycode serve` is running. `CheckContextHealth` (`session/pruning.go`) for runtime health. Periodic metric snapshots to disk. | **Health Cards:** Metric snapshots written to disk; live stats from Prometheus when serve is running. | The "Analytic": latency, cost, and health metrics. | Metadata: Resource usage and performance telemetry. | Operational (Runtime + Snapshots) |
| **L7** | **Governance** | The moral and policy gate: enforces organizational rules, safety guardrails, and privacy (e.g., redacting credentials, blocking restricted commands). | Pattern-matching policy engine (`runtime/policy/engine.go`) with wildcard tool/path matching and priority-based evaluation. | **Policy Manual:** Policy rules as tagged markdown files (e.g., `#policy_no_rm`). | The "Censor": strict policy guardrails that prevent restricted or unsafe actions. | Gatekeeper: Permission and compliance checks. | Regulatory (Static) |
| **L8** | **Collective** | The hive mind: real-time synchronization of intelligence between independent agents. Sharing learned "intuition" rather than just raw data. | Not yet implemented. Protocols identified: [A2A](https://developers.googleblog.com/en/a2a-a-new-era-of-agent-interoperability/) (Agent-to-Agent), [AGP](https://agentgatewayprotocol.dev/) (Agent Gateway Protocol), Cisco ANP — under AAIF governance. | **Swarm Insights:** (Planned) Shared memos from other agents in the network. | The "Swarm": shared insights and learned patterns from other agents in the network. | Input: Cross-agent collaborative intelligence. | Distributed (Global) |
| **L9** | **Counterfactual** | Multi-verse simulation: managing "What-If" scenarios. The agent "remembers" simulated failures to avoid them in reality (Predictive Verification). | Not yet implemented. [E2B](https://e2b.dev/) sandbox identified for future use. | **Sim-Logs:** (Planned) "What-If" failure modes recorded as memos. | The "Multi-Verse": summaries of failed or successful branches from pre-action simulations. | Simulation: Predicted outcomes used for verification. | Predictive (Pre-Act) |

## SOTA Landscape (2025-2026)

### Memory Framework Convergence

The field has converged on three standard memory scopes across all major frameworks: **episodic** (what happened), **semantic** (what is known), and **procedural** (how to act). The key differentiators are now in lifecycle management, retrieval strategies, and storage representations. "Context engineering" has replaced "prompt engineering" as the defining AI skill of 2026 (Gartner; coined by Phil Schmid at Google DeepMind).

#### Letta (formerly MemGPT)

Three-tier OS-inspired model: **core memory** (always in-context, like RAM), **archival memory** (external vector store, like disk), **recall memory** (conversation history). Agents self-edit their memory by calling memory functions during reasoning loops. The Conversations API enables shared memory across parallel user interactions.

**Letta Code** (March-April 2026): memory-first coding agent, #1 on Terminal-Bench among model-agnostic open-source agents, portable across Claude/GPT/Gemini/Kimi. Released **Letta Evals** for testing stateful agents.

- Self-editing paradigm: more adaptive but less predictable than passive extraction
- [letta.com/blog/agent-memory](https://www.letta.com/blog/agent-memory) | [letta.com/blog/letta-code](https://www.letta.com/blog/letta-code)

#### Mem0 and Mem0g

Three memory scopes: **user**, **session**, and **agent**, backed by a hybrid store (vectors + graph relationships + key-value). Passive extraction via `add()` triggers a pipeline that decides what facts to store. v1.0.0 added explicit procedural memory support. Published "State of AI Agent Memory 2026."

**Mem0g** (graph-enhanced variant) stores memories as directed labeled graphs. Benchmarks: 58.13% accuracy vs OpenAI's 21.71% on temporal reasoning; 67.13% on LOCOMO; p95 search latency 0.200s; ~1,764 tokens per conversation vs 26,031 for full-context.

- Passive extraction: more predictable and token-efficient than self-editing
- 19 vector store backends supported; 21 framework integrations
- [arxiv.org/abs/2504.19413](https://arxiv.org/abs/2504.19413) | [mem0.ai/research](https://mem0.ai/research)

#### Zep / Graphiti

Temporal knowledge graph architecture for agent memory enabling **persistent agent identity**. Core engine is **Graphiti** — a temporally-aware knowledge graph that synthesizes unstructured conversational data and structured business data while maintaining historical relationships.

Three hierarchical graph tiers: **episode subgraph**, **semantic entity subgraph**, **community subgraph**. Bi-temporal model tracks when events occurred AND when they were ingested; every edge has explicit validity intervals. **Graphiti MCP Server v1.0** (November 2025) with hundreds of thousands of weekly MCP users. P95 graph search dropped to 150ms. Zep Community Edition deprecated (April 2025).

- 94.8% on DMR benchmark (vs 93.4% for MemGPT); 18.5% accuracy improvement and 90% latency reduction vs baselines
- ~14,000 GitHub stars; 25,000 weekly PyPI downloads
- [arxiv.org/abs/2501.13956](https://arxiv.org/abs/2501.13956) | [github.com/getzep/graphiti](https://github.com/getzep/graphiti)

#### LangMem SDK (LangChain)

Supports episodic, semantic, and procedural memory types within the LangGraph ecosystem. Provides memory management tools agents use during active conversations plus a background memory manager for automatic extraction/consolidation. Multiple prompt-update algorithms: metaprompt, gradient, prompt_memory. Unique feature: agents rewrite their own system prompt over time via prompt optimization.

- [blog.langchain.com/langmem-sdk-launch](https://blog.langchain.com/langmem-sdk-launch/) | [github.com/langchain-ai/langmem](https://github.com/langchain-ai/langmem)

#### CrewAI Memory

Four-component architecture: **short-term**, **long-term**, **entity**, and **procedural** memory. Short-term memory integrated with ChromaDB using RAG for session-specific context.

### Memory-as-OS Paradigm

A major 2025-2026 trend: treating memory as a **manageable system resource** with explicit lifecycle operations, rather than an implicit side-effect of conversation.

#### MemOS (Shanghai Jiao Tong / Zhejiang University)

First "Memory Operating System" — unifies plaintext, activation-based, and parameter-level memories. **MemCube** abstraction encapsulates content + metadata (provenance, versioning); composable, migratable, fusable.

- v2.0 "Stardust" (December 2025): KB features, memory feedback, multi-modal memory, tool memory for agent planning, Redis Streams scheduling
- v2.0 OpenClaw Plugin (March 2026): hosted memory service, 72% lower token usage, multi-agent memory sharing
- 159% improvement in temporal reasoning over OpenAI's global memory; 38.97% overall accuracy gain; 60.95% token overhead reduction on LoCoMo
- [github.com/MemTensor/MemOS](https://github.com/MemTensor/MemOS) | [arxiv.org/abs/2507.03724](https://arxiv.org/abs/2507.03724)

#### MemoryOS (BAI-LAB)

Separate project from MemTensor's MemOS. EMNLP 2025 Oral. 159% temporal reasoning improvement over OpenAI's global memory.

- [github.com/BAI-LAB/MemoryOS](https://github.com/BAI-LAB/MemoryOS)

#### EverMemOS

Self-Organizing Memory Operating System for structured long-horizon reasoning. Engram-inspired memory lifecycle: Episodic Trace Formation segments dialogue into MemCells with episodes, atomic facts, and time-bounded foresight. 83% LongMemEval, 92.3% LoCoMo. Cloud launch February 2026.

- [arxiv.org/pdf/2601.02163](https://arxiv.org/pdf/2601.02163)

### Graph-Based Memory Architectures

Graph-based memory has emerged as the dominant representation for structured agent memory, surpassing flat vector stores for relational and temporal reasoning.

#### MAGMA (Multi-Graph Agentic Memory Architecture, January 2026)

Represents each memory item across **orthogonal semantic, temporal, causal, and entity graphs**. Retrieval is formulated as policy-guided traversal over these relational views, enabling query-adaptive selection.

- [arxiv.org/abs/2601.03236](https://arxiv.org/abs/2601.03236)

#### GraphRAG (Microsoft)

Leverages structural information across entities for query-focused summarization; moves from local passages to global structure. LinearRAG (efficient variant) and GraphRAG Benchmark accepted at ICLR 2026.

- Knowledge graph extraction costs 3-5x more than baseline RAG; entity recognition accuracy 60-85% depending on domain
- [microsoft.com/en-us/research/project/graphrag](https://www.microsoft.com/en-us/research/project/graphrag/)

### RL-Driven Memory

#### MemRL (January 2026)

Self-evolving agents via runtime RL on episodic memory. Decouples stable cognitive reasoning (frozen LLM) from dynamic episodic memory. Two-Phase Retrieval: filter by semantic relevance, then select by learned Q-values (utility). Formalizes LLM-memory interaction as a Markov Decision Process.

- Outperforms SOTA on HLE, BigCodeBench, ALFWorld, and Lifelong Agent Bench
- [arxiv.org/abs/2601.03192](https://arxiv.org/abs/2601.03192) | [github.com/MemTensor/MemRL](https://github.com/MemTensor/MemRL)

#### Memory-R1 (January 2026)

RL-trained Memory Manager (ADD/UPDATE/DELETE/NOOP) + Answer Agent using PPO/GRPO.

- [arxiv.org/abs/2508.19828](https://arxiv.org/abs/2508.19828)

### Memory Compression and Forgetting

#### SimpleMem (January 2026)

Three-stage pipeline: (1) Semantic Structured Compression with entropy-aware filtering, (2) Recursive Memory Consolidation (async, merges related fragments), (3) Adaptive Query-Aware Retrieval (adjusts scope by query complexity).

- 26.4% average F1 improvement; up to 30x reduction in inference-time token consumption
- Supports text and multimodal memory
- [github.com/aiming-lab/SimpleMem](https://github.com/aiming-lab/SimpleMem)

#### Forgetting Mechanisms

- Temporal heuristics (FIFO, LRU), importance-aware methods (priority decay), reflective consolidation (summary-based compression), hybrid staging
- Ebbinghaus Forgetting Curve applied: continuous decay rates on stored vectors with exponential decay unless reinforced
- "Forgetful but Faithful" (2025): cognitive memory architecture with privacy-aware forgetting for generative agents
- [arxiv.org/html/2512.12856v1](https://arxiv.org/html/2512.12856v1)

### Hierarchical Memory (H-MEM)

Multi-level memory organized by degree of semantic abstraction. Separates short-term interaction from long-term abstraction while controlling semantic drift across temporal intervals.

- [arxiv.org/abs/2507.22925](https://arxiv.org/abs/2507.22925)

### Multi-Agent Memory

Computer-architecture-inspired three-layer hierarchy: **I/O**, **cache**, and **memory**. Distinguishes shared vs. distributed memory paradigms for multi-agent systems.

- [arxiv.org/html/2603.10062v1](https://arxiv.org/html/2603.10062v1)

### Novel Architectures

#### A-MEM (NeurIPS 2025)

Zettelkasten-inspired interconnected knowledge networks. Organizes memories as atomic, interconnected notes with bidirectional links.

- [arxiv.org/abs/2502.12110](https://arxiv.org/abs/2502.12110)

#### MemPalace (April 2026)

Method of loci architecture: Wing/Room/Hall hierarchy. 19 MCP tools. 96.6% on LongMemEval (contested). Fully offline: ChromaDB + SQLite. 36K GitHub stars in 5 days. 30x compression claimed.

- [github.com/milla-jovovich/mempalace](https://github.com/milla-jovovich/mempalace)

#### Memvid

Encodes AI memory as video frames using H.264/H.265 codecs; single-file `.mv2` format with no database required. Claims +35% SOTA on LoCoMo, +76% multi-hop, +56% temporal reasoning; 0.025ms P50 retrieval; 1,372x throughput vs standard RAG; 10x compression. Suited for offline, edge, or single-user applications.

- [github.com/memvid/memvid](https://github.com/memvid/memvid)

### Auto Dream Pattern

Background memory consolidation, a pattern emerging in modern agentic tools (late March 2026). Converts relative dates to absolute, deletes contradicted facts, merges overlapping entries. Triggers after idle period + threshold of new records. ycode's Dreamer (`memory/dream.go`) implements this pattern with 30-minute cycles.

### Anthropic Memory Tool API (Beta)

Client-side file-based memory with `/memories` directory. 84% token reduction in extended workflows.

### Context Window Evolution (2026)

| Model | Context Window | Notes |
|-------|---------------|-------|
| Claude Opus/Sonnet 4.6 | **1M GA** (March 2026) | No surcharge |
| GPT-5.4 | 272K / 1M expandable | 2x pricing above 272K |
| Gemini 3.1 Pro | 1M input, 65K output | February 2026 |

### Taxonomic Survey: Memory in the Age of AI Agents (December 2025)

Comprehensive taxonomy: **Forms-Functions-Dynamics**.

- **Forms** (storage medium): token-level (explicit, editable), parametric (embedded in weights), latent (continuous vectors / KV-cache)
- **Functions** (purpose): factual, experiential, working memory
- **Dynamics** (lifecycle): Formation → Evolution → Retrieval operators
- Emerging frontiers: memory automation, RL integration, multimodal memory, multi-agent memory, trustworthiness
- [arxiv.org/abs/2512.13564](https://arxiv.org/abs/2512.13564) | [Paper list: github.com/Shichun-Liu/Agent-Memory-Paper-List](https://github.com/Shichun-Liu/Agent-Memory-Paper-List)
- ICLR 2026 accepted a dedicated **MemAgents Workshop** on "Memory for LLM-Based Agentic Systems" (April 27, Rio)

## Priorart Comparison

| Feature | ycode | Gemini CLI | OpenClaw | OpenHands | Cline | Aider |
|---------|-------|-----------|----------|-----------|-------|-------|
| **Session persistence** | JSONL (256KB rotation) | Chat recording + extraction state | Pi transcripts + JSONL events | Event stream + SQL | VS Code storage API | Markdown files |
| **Context defense** | 3-layer (mask → prune → compact) | 4-phase compression with self-correcting validation | Token-budget compaction (8K min) | 9-type condenser pipeline | Model-specific window budgets | Proportional chat budget |
| **Long-term memory** | File-based (4 types, 2 scopes) + Dreamer | Hierarchical (Global→Extension→Project→User) | LanceDB + memory-wiki (Obsidian) | Microagents (keyword-triggered) | Per-task directory storage | Git history as implicit memory |
| **Tool output mgmt** | Head+tail distillation + disk offload | SHA-256 content-hash compression | Token sanitization during compaction | Observation masking condenser | Streaming with interception | None |
| **Memory search** | Vector (chromem-go) + FTS (Bleve) + keyword | Hierarchical GEMINI.md discovery | LanceDB embeddings | — | — | LlamaIndex for docs only |
| **File-first auditability** | **All artifacts as markdown/JSONL, managed by Memos wiki** | GEMINI.md files (no wiki) | memory-wiki (requires Obsidian) | Event logs (not human-readable) | JSON files | Markdown history |
| **Spatial memory** | — | — | — | — | — | tree-sitter repo map |

**Key differentiator:** ycode is the only agent where all memory artifacts are human-readable markdown files on disk with wiki-style management via Memos, providing full auditability without requiring external tools.

## Gap Analysis

### Critical and High-Priority Gaps

| Gap | Reference Implementation | Priority | Notes |
|-----|--------------------------|----------|-------|
| Catch-up indexing on serve startup | (Designed but not implemented) | **Critical** | Core to file-first architecture: scan for new/modified files, rebuild vector/FTS/Memos indexes |
| Periodic metric/trace snapshots to disk | (Designed but not implemented) | **High** | Ensures observability data survives service restarts |
| Tree-sitter repo map | Aider (spatial memory) | **High** | "Spatial memory" of codebase structure; always-in-context function/class signatures |
| Memory search tool for agent | (vector+bleve exist but no agent-facing tool) | **High** | Agent cannot explicitly search its own persistent memories during reasoning |
| Session forking/branching | OpenCode (revert-compact) | Medium | "What-if" session exploration with rollback |
| Skill extraction from sessions | Gemini CLI (MemoryService) | Medium | Auto-extract reusable skills/patterns from completed tasks |
| Graph-based memory | Graphiti, Mem0g | Low | Temporal knowledge graphs for relational reasoning |
| RL-driven retrieval | MemRL, Memory-R1 | Low | Q-value scoring instead of pure semantic similarity |

### Already Implemented Well

- Agent-requested compaction (`tools/compact_context.go`)
- Background dreaming/consolidation (`memory/dream.go`, 30-min interval)
- 3-layer context defense with observation masking
- Tool output distillation with disk offload
- Prompt caching with fingerprinting + cache warming
- Differential context injection for non-caching providers
- Persistent file-based memory with types, scopes, temporal decay
- Multi-backend search (vector + FTS + keyword)
- Memos REST API client + tool handlers (`memos/client.go`, `tools/memos.go`)
- CJK-aware token estimation
- Context health monitoring with 4 levels

## Emerging Standards and Protocols

### MCP (Model Context Protocol)

Anthropic (November 2024); now governed by Linux Foundation's AAIF. 97 million monthly SDK downloads by February 2026; 500+ servers. Adopted by Anthropic, OpenAI, Google, Microsoft, Amazon. Defines how agents access external tools, APIs, and data — including memory servers.

### A2A (Agent-to-Agent Protocol)

Google (April 2025); donated to Linux Foundation June 2025. IBM's ACP merged into A2A in August 2025. **v1.0** (early 2026): production-grade with Signed Agent Cards, multi-tenancy, multi-protocol bindings (JSON-RPC + gRPC), version negotiation. 150+ organizations, 22K+ stars. In production on Azure AI Foundry and Amazon Bedrock AgentCore.

### AGP (Agent Gateway Protocol)

A2A extension by AGNTCY. BGP-inspired hierarchical routing for enterprise multi-agent systems. gRPC/HTTP2 with mTLS + RBAC. Cross-system communication protocol for routing agent interactions across heterogeneous platforms and infrastructure boundaries.

### ANP (Agent Network Protocol)

Cisco's contribution to the agent interoperability ecosystem. Focuses on network-level agent discovery and communication primitives.

### AAIF (Agentic AI Foundation)

Linux Foundation; launched December 2025; co-founders: OpenAI, Anthropic, Google, Microsoft, AWS, Block. Governs both MCP and A2A as complementary standards.

### MCP Memory Server Implementations

| Server | Approach | Notable |
|--------|----------|---------|
| **OpenMemory MCP (Mem0)** | Private, local-first memory server | Shared persistent memory layer across MCP-compatible tools |
| **Hindsight** | Structured fact extraction + entity resolution + embeddings | 91.4% on LongMemEval (multi-session: 21.1% → 79.7%) |
| **MemPalace** | Method of loci architecture, 19 MCP tools, ChromaDB + SQLite | 96.6% LongMemEval (contested); 36K GitHub stars |
| **engram** | Pure Go, single binary, SQLite + FTS5, MCP + HTTP + CLI + TUI | Works with Claude Code, Gemini CLI, Codex, Cursor. No CGO. |
| **mcp-memory-service** | Persistent memory with semantic search + web dashboard | Inter-agent messaging bus; autonomous consolidation |
| **agentmemory** | Silent capture, compress, inject for Claude Code / Cursor / Gemini CLI | Passive memory augmentation |
| **MCP Memory Keeper** | SQLite + knowledge graph extraction + semantic search | Progressive compression |

## Benchmarks

| Benchmark | Scope | Details |
|-----------|-------|---------|
| **LoCoMo** | ~1,500-2,000 QA pairs | Single-hop, multi-hop, temporal, open-domain, adversarial; up to 32 sessions / ~600 turns / ~16,000 tokens |
| **LongMemEval** | 500 manually created questions | Information extraction, multi-session reasoning, temporal reasoning, knowledge updates, abstention. Top 2026: MemPalace 96.6% (contested), Hindsight+Gemini-3 91.4%, EverMemOS 83.0% |
| **DMR** | Dialog memory retrieval | Used by Zep/Graphiti for benchmarking |
| **Terminal-Bench** | Coding agent benchmark | Memory-first agents (Letta Code) lead |
| **MemoryAgentBench** | Incremental multi-turn interactions | ICLR 2026. Evaluates memory via progressive dialogue. [github.com/HUST-AI-HYZ/MemoryAgentBench](https://github.com/HUST-AI-HYZ/MemoryAgentBench) |
| **AMA-Bench** | Long-horizon agentic memory | February 2026. [arxiv.org/html/2602.22769v1](https://arxiv.org/html/2602.22769v1) |

**Caveat:** LoCoMo and LongMemEval were designed for 32k context windows. With million-token context windows now standard, naive "dump everything into context" approaches score competitively, raising questions about whether retrieval-based memory demonstrates clear value on these benchmarks. New benchmarks (MemoryAgentBench, AMA-Bench) address this gap.

## Evaluation Framework

### Baseline Metrics (Capture Before Any Change)

- **Token consumption**: input/output/cache read/write tokens per session
- **Compaction frequency**: compactions per session, summary quality (information retention score)
- **Memory retrieval**: precision/recall for vector + Bleve searches
- **Session longevity**: messages before context exhaustion
- **Task completion**: success rate on representative multi-file edit workloads

### Automated Regression Tests

- **Unit tests**: each memory component has `*_test.go` alongside source
- **Property-based tests**: compaction output always ≤ budget; key facts (Primary Goal, Working Set) preserved
- **Fuzz tests**: empty sessions, huge tool outputs, CJK-heavy content, rapid compaction cycles
- **Golden-file tests**: known conversation → expected compaction summary; diff against baseline

### Benchmark Suite

- **Internal coding-agent benchmark**: multi-file edit tasks with session length tracking, token consumption, and task completion rate
- **Adapted LoCoMo/LongMemEval**: coding-context variants testing multi-session fact retention (e.g., "What file did you edit in session 3?")
- **A/B comparison harness**: same workload on old vs new system; compare token usage, compaction quality, task completion

### Canary / Shadow Mode

- **Dual-write**: run new memory path alongside old, compare outputs without affecting user
- **Feature flags**: gradual rollout via config (`settings.json`)
- **Automatic rollback**: if token consumption increases >10% or task completion drops

### Observability Guardrails

- **Prometheus counters**: `ycode_compaction_total`, `ycode_memory_recall_latency_seconds`, `ycode_context_tokens_estimate`
- **OTEL traces**: memory retrieval paths with span timing
- **Alerting**: compaction frequency spike, memory search latency >500ms, cache hit rate drop
- **Disk snapshots**: all metrics snapshotted periodically so guardrails work even without live Prometheus

## Key Trends (2025-2026)

1. **"Context engineering" replaces "prompt engineering"** — Gartner and Google DeepMind position context management as the defining AI skill of 2026
2. **Graph-based memory is ascendant** — Zep/Graphiti, Mem0g, MAGMA all use knowledge graphs with temporal awareness, surpassing flat vector stores for relational reasoning
3. **Memory-as-OS abstraction** — MemOS, EverMemOS, and Letta treat memory as a system resource with explicit lifecycle management (MemCube, engrams)
4. **Self-editing vs passive extraction tradeoff** — Letta agents self-edit memory (more adaptive); Mem0 extracts passively (more predictable, token-efficient)
5. **RL-driven memory** — MemRL and Memory-R1 formalize memory retrieval as MDPs with Q-value scoring; decouple reasoning from memory evolution
6. **MCP as universal connector** — 97M monthly downloads; 500+ servers; every major AI provider adopted; MCP memory servers emerging as standard pattern
7. **Standards consolidation under AAIF** — MCP + A2A unified under Linux Foundation governance; A2A v1.0 in production
8. **Auto Dream pattern** — background memory consolidation (Claude Code, ycode Dreamer) becoming standard for persistent agents
9. **Compression and forgetting** — entropy-aware filtering, Ebbinghaus decay curves, and progressive consolidation now standard techniques
10. **Multi-agent memory** — computer-architecture-inspired shared/distributed paradigms; A2A enables cross-agent memory sharing
11. **Million-token context challenges benchmarks** — LoCoMo/LongMemEval designed for 32K; new benchmarks (MemoryAgentBench, AMA-Bench) needed
12. **File-first memory** — Anthropic Memory Tool API, AGENTS.md (60K+ repos), ycode Memos demonstrate file-based memory as viable alternative to databases

## Industry Stack Cross-Reference

The [AIMultiple 7-layer agentic AI stack](https://aimultiple.com/agentic-ai-stack) maps to our 9-layer model as follows:

| Industry Layer | Description | ycode Layers | Strategic Moat |
|:---------------|:------------|:-------------|:---------------|
| L1 Foundation Model Infrastructure | Models, compute, data, APIs | (External dependency) | Low — commoditized by hyperscalers |
| L2 Agent Runtime & Infrastructure | Execution, memory systems, embedding stores | L1 Working, L2 Episodic | Medium — defensible through domain specialization |
| L3 Protocol & Interoperability | A2A, ANP, AGP, MCP | L8 Collective | Low — standards commoditize quickly |
| L4 Orchestration | Multi-agent coordination, prompt routing, memory preservation, RAG | L4 Procedural, L5 Reflective | Medium — workflow coupling creates lock-in |
| L5 Tooling & Enrichment | RAG, vector DBs, knowledge bases, tool invocation | L3 Semantic, L4 Procedural | **High** — 2-5 years to build; plugin ecosystems create platform lock-in |
| L6 Applications | Co-pilots, agent teammates | (Application layer) | Low — crowded, interchangeable; only vertical data-rich apps differentiate |
| L7 Observability & Governance | Monitoring, safety, deployment, privacy, registries | L6 Observability, L7 Governance | **High** — deep technical expertise, long development cycles |

**Key insight:** The highest-moat layers (Tooling/Enrichment and Observability/Governance) align with our L3-L4 and L6-L7 — exactly where ycode's file-first Memos wiki, multi-backend search (chromem-go + Bleve), 3-layer context defense, and policy engine create defensible depth.

## Strategic Integration

- **File-First Memos Architecture:** All memory artifacts (sessions, compaction summaries, memories, traces, metric snapshots) written as markdown/JSONL to disk; Memos indexes and organizes them as a wiki when `ycode serve` is running. Catch-up indexing on serve startup ensures nothing is lost.
- **The Go Logic:** The "Plan, Act, Verify" loop lives at L4 (Procedural), while OTEL/OTLP traces power L2 (Episodic) and L6 (Observability) simultaneously — with periodic snapshots to disk for offline durability.
- **The Moat:** File-first auditability via Memos wiki + multi-backend search (vector + FTS + keyword) + 3-layer context defense + structured compaction with intent summaries + policy engine for governance — delivers deep observability and human-viewable history while remaining lightweight and permissively licensed.
- **Graph Memory:** Consider Graphiti or Mem0g for L3 (Semantic) to gain temporal awareness and relational reasoning over flat vector retrieval.
- **Memory OS:** Adopt MemCube-style abstractions for uniform memory lifecycle management across layers — versioning, provenance, and composability.
- **MCP Memory Servers:** Expose ycode's memory layers as MCP-compatible endpoints to enable interoperability with the broader agent ecosystem.
- **RL-Driven Retrieval:** Explore MemRL's Q-value approach for L5 (Reflective) to learn which memories are most useful rather than relying solely on semantic similarity.

## References

### Core Infrastructure (L1-L4)

- **L1 (Working Memory):** JSONL session persistence with context window management (`session/session.go`).
- **L2 (Episodic Memory):** [OpenTelemetry (OTEL)](https://opentelemetry.io/) Specification for traces and logs that form agent "episodes."
- **L3 (Semantic Memory):** [chromem-go](https://github.com/philippgille/chromem-go) for Go-native vector storage; [Bleve v2](https://blevesearch.com/) for full-text search with BM25; Graphiti for temporal knowledge graphs.
- **L4 (Procedural Memory):** [Model Context Protocol (MCP)](https://modelcontextprotocol.io) as the standard for representing agent tools and skills to LLMs; now under AAIF governance.

### Observability and Traceability (L6 + Cross-Layer)

- **L6 (Observability):** [Prometheus](https://prometheus.io/) Go client for custom agent metrics (retrieval latency, token usage, plan-phase timing). Periodic disk snapshots for offline durability.
- **Traceability:** [Memos](https://usememos.com/) as the unified wiki/traceability layer across all layers — lightweight, self-hosted, API-driven, tag-based organization. File-first: always on disk, service-enhanced when available.

### Architectural Frameworks (L5-L8)

- **SOTA Memory Systems:** Letta/Letta Code, Mem0/Mem0g, Zep/Graphiti, LangMem SDK, MemOS, MemoryOS, EverMemOS for autonomous memory management.
- **Memory Research:** "Memory in the Age of AI Agents" survey ([arxiv 2512.13564](https://arxiv.org/abs/2512.13564)); MAGMA ([arxiv 2601.03236](https://arxiv.org/abs/2601.03236)); MemRL ([arxiv 2601.03192](https://arxiv.org/abs/2601.03192)); Memory-R1 ([arxiv 2508.19828](https://arxiv.org/abs/2508.19828)); A-MEM ([arxiv 2502.12110](https://arxiv.org/abs/2502.12110)); SimpleMem ([github](https://github.com/aiming-lab/SimpleMem)); H-MEM ([arxiv 2507.22925](https://arxiv.org/abs/2507.22925)).
- **L7 (Governance):** Pattern-matching policy engine (`runtime/policy/engine.go`) for implementing gatekeeper logic required for safe agentic actions.
- **L8 (Collective Intelligence):** [A2A v1.0](https://a2a-protocol.org/latest/specification/) under AAIF; standardized agent discovery and cross-agent memory sharing.

### Standards and Protocols

- **AAIF (Agentic AI Foundation):** Linux Foundation; governs MCP + A2A. Co-founders: OpenAI, Anthropic, Google, Microsoft, AWS, Block.
- **MCP:** [modelcontextprotocol.io](https://modelcontextprotocol.io) — 97M monthly SDK downloads; 500+ servers.
- **A2A:** [a2a-protocol.org](https://a2a-protocol.org/latest/specification/) — v1.0 with Signed Agent Cards; IBM ACP merged August 2025.
- **AGP:** [Agent Gateway Protocol](https://agentgatewayprotocol.dev/) — BGP-inspired hierarchical routing for enterprise multi-agent systems.
- **ANP:** Cisco Agent Network Protocol — network-level agent discovery.
- **Industry Stack:** [AIMultiple Agentic AI Stack](https://aimultiple.com/agentic-ai-stack) — 7-layer industry reference architecture.

### Benchmarks and Evaluation

- **LoCoMo:** [snap-research.github.io/locomo](https://snap-research.github.io/locomo/)
- **LongMemEval:** [arxiv.org/pdf/2410.10813](https://arxiv.org/pdf/2410.10813) (ICLR 2025)
- **MemoryAgentBench:** [github.com/HUST-AI-HYZ/MemoryAgentBench](https://github.com/HUST-AI-HYZ/MemoryAgentBench) (ICLR 2026)
- **AMA-Bench:** [arxiv.org/html/2602.22769v1](https://arxiv.org/html/2602.22769v1) (February 2026)
- **ICLR 2026 MemAgents Workshop:** [sites.google.com/view/memagent-iclr26](https://sites.google.com/view/memagent-iclr26/)
- **Agent Memory Benchmark Manifesto:** [hindsight.vectorize.io](https://hindsight.vectorize.io/blog/2026/03/23/agent-memory-benchmark)

### Go Libraries for Agent Memory

- **engram:** Pure Go, SQLite+FTS5, MCP+HTTP+CLI+TUI. [github.com/Gentleman-Programming/engram](https://github.com/Gentleman-Programming/engram)
- **Google ADK:** Memory interface for cross-session per-user memory. [pkg.go.dev/google.golang.org/adk/agent](https://pkg.go.dev/google.golang.org/adk/agent)

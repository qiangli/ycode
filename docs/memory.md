# Memory

## Overview

This document consolidates the architectural details, implementation specifics, and theoretical research for the agentic memory model used in the Go-based ycode stack. Updated with SOTA developments through early 2026.

## Quick Reference (5 Core Layers)

| Layer | Type | Purpose | Implementation | LLM Perspective | I/O Category | Persistence |
|-------|------|---------|----------------|-----------------|--------------|-------------|
| L1 | Working | Immediate reasoning and current task state | Matrix Room State / Active Context window | The "Conversation": literal text of recent messages | Input: Current message + recent history | Volatile (Current Session) |
| L2 | Episodic | Specific past experiences and event logs | OTEL Traces / Vectorized event history | The "Flashback": summarized snippet of a past event | Reference: Retrievable context for the prompt | Long-term (Searchable) |
| L3 | Semantic | Technical facts and system knowledge | Vector DB (Dgraph/ArcadeDB) + Knowledge Graphs | The "Knowledge": static facts or documentation | Reference: Static data injected into the prompt | Permanent (Knowledge Base) |
| L4 | Procedural | "How-to" skills and execution logic | Go Functions / Tool Definitions (MCP) | The "Toolkit": JSON schema of available functions | Output: Tool Calls (JSON) sent back to Go | Fixed (Logic/Skills) |
| L5 | Reflective | High-level strategies and self-optimization | Summarized Insights DB / RL-driven patterns | The "Strategy": distilled lessons and corrected behaviors | Feedback Loop: Meta-analysis of episodic patterns | Evolving (Continuous) |

## Full 13-Layer Agentic Memory Reference

| Layer | Type | Purpose | Implementation | LLM Perspective | I/O Category | Persistence |
|:------|:-----|:--------|:---------------|:----------------|:-------------|:------------|
| L1 | Working | Immediate context and active reasoning: the "RAM" of the agent. Holds current task goals, recent conversation turns, and Plan/Act/Verify loop state. | Zulip Topic-Based Threading or Matrix Room State. Managed via local Go variables during runtime. | The "Conversation": literal text of the last few messages and active thread context. | Input: Current message + immediate session history. | Volatile (Current Session) |
| L2 | Episodic | Auto-biographical event logs: chronological records of specific past actions and outcomes. Answers "What did I do last time?" to avoid repeating errors. | OTEL Traces (OpenTelemetry) stored as vectorized event logs. Trace IDs map to specific historical agent "episodes." | The "Flashback": summarized snippet or re-hydrated log of a specific past event injected into the prompt. | Reference: Retrievable context for the current prompt. | Long-term (Searchable) |
| L3 | Semantic | The internal encyclopedia: general facts, technical documentation, and concept relationships (e.g., Go syntax, OTLP specs) independent of specific events. | Vector Databases like Pinecone, Dgraph, or ArcadeDB. Utilizes RAG (Retrieval-Augmented Generation). | The "Knowledge": static facts, code snippets, or documentation provided as reference text. | Reference: Static data injected via RAG. | Permanent (Knowledge Base) |
| L4 | Procedural | The internal instruction manual: "How-to" skills and execution logic. Governs the steps for debugging, deploying, or interacting with system APIs. | Go Functions and Tool Definitions. Hard-coded logic gates that the agent triggers to perform physical actions. | The "Toolkit": JSON schema defining available functions and tools the LLM is permitted to call. | Output: Tool Calls (JSON) sent back to your Go binary. | Fixed (Logic/Skills) |
| L5 | Reflective | Self-optimization and metacognition: high-level abstractions and "lessons learned." The agent analyzes its own episodic patterns to refine future strategies. | Summarized Insights DB. A "Background Processor" in Go that distills L2 (Episodic) data into high-level rules. | The "Strategy": distilled lessons, optimized plans, and corrected behaviors based on self-reflection. | Feedback Loop: Meta-analysis of past performance. | Evolving (Continuous) |
| L6 | Observability | Self-awareness and efficiency: monitoring the agent's own health, read/write costs, and latency. Prevents context window bloat and manages token budgets. | VictoriaMetrics for metrics; Langfuse for performance tracing. Integration with OTEL/OTLP for real-time monitoring. | The "Analytic": internal system metrics and health status. | Metadata: Resource usage and performance telemetry. | Operational (Runtime) |
| L7 | Governance | The moral and policy gate: enforces organizational rules, safety guardrails, and privacy (e.g., redacting credentials, blocking restricted commands). | Open Policy Agent (OPA) or dedicated Governance Engines that intercept and validate LLM outputs. | The "Censor": strict policy guardrails that prevent restricted or unsafe actions. | Gatekeeper: Permission and compliance checks. | Regulatory (Static) |
| L8 | Collective | The hive mind: real-time synchronization of intelligence between independent agents. Sharing learned "intuition" rather than just raw data. | Interoperability Protocols like A2A (Agent-to-Agent) or Google/IBM shared messaging standards. | The "Swarm": shared insights and learned patterns from other agents in the network. | Input: Cross-agent collaborative intelligence. | Distributed (Global) |
| L9 | Counterfactual | Multi-verse simulation: managing "What-If" scenarios. The agent "remembers" simulated failures to avoid them in reality (Predictive Verification). | Parallel sandbox execution environments (e.g., E2B) that run simulations before the final "Act." | The "Multi-Verse": summaries of failed or successful branches from pre-action simulations. | Simulation: Predicted outcomes used for verification. | Predictive (Pre-Act) |
| L10 | Substrate | Universal archival: hardware-independent memory. Storing agent states in non-silicon mediums like Synthetic DNA or high-density optical storage. | Synthetic DNA storage or permanent immutable cold-storage ledgers. | The "Archive": deep historical recovery of data from previous "lives" or ancestral agents. | Input: Historical recovery from archival storage. | Eternal (Millennial) |
| L11 | Planck-Scale | Information physics: based on the Holographic Principle; treating information as a fundamental 2D property of the 3D universe. | Theoretical: information-theoretic limits of bit density in space-time. | The "Pixel": fundamental "bits" of reality that the agent perceives as building blocks of data. | Foundation: Info-physics primitives. | Cosmic |
| L12 | Entangled | Quantum non-locality: zero-latency memory synchronization between distant nodes via quantum entanglement (Quantum RAG). | Quantum Network Nodes / Entanglement Distribution. | The "Sync": instantaneous state updates across any distance without signal travel time. | Input: Non-local quantum data stream. | Quantum |
| L13 | Universal | The Omega Point: the final state where the universe itself acts as a self-aware computer, storing total history and future simulations. | Universal Computation where all matter is converted into "Computronium." | The "Final Agent": total, absolute knowledge of every atomic state in existence. | System: The Universe as the final memory hub. | Absolute |

## SOTA Landscape (2025-2026)

### Memory Framework Convergence

The field has converged on three standard memory scopes across all major frameworks: **episodic** (what happened), **semantic** (what is known), and **procedural** (how to act). The key differentiators are now in lifecycle management, retrieval strategies, and storage representations.

#### Letta (formerly MemGPT)

Three-tier OS-inspired model: **core memory** (always in-context, like RAM), **archival memory** (external vector store, like disk), **recall memory** (conversation history). Agents self-edit their memory by calling memory functions during reasoning loops. The Conversations API enables shared memory across parallel user interactions. Letta Code ranks #1 on Terminal-Bench as a memory-first coding agent.

- Self-editing paradigm: more adaptive but less predictable than passive extraction
- [letta.com/blog/agent-memory](https://www.letta.com/blog/agent-memory)

#### Mem0 and Mem0g

Three memory scopes: **user**, **session**, and **agent**, backed by a hybrid store (vectors + graph relationships + key-value). Passive extraction via `add()` triggers a pipeline that decides what facts to store. v1.0.0 added explicit procedural memory support.

**Mem0g** (graph-enhanced variant) stores memories as directed labeled graphs. Benchmarks: 58.13% accuracy vs OpenAI's 21.71% on temporal reasoning; 67.13% on LOCOMO; p95 search latency 0.200s; ~1,764 tokens per conversation vs 26,031 for full-context.

- Passive extraction: more predictable and token-efficient than self-editing
- 19 vector store backends supported
- [arxiv.org/abs/2504.19413](https://arxiv.org/abs/2504.19413) | [mem0.ai/research](https://mem0.ai/research)

#### Zep / Graphiti

Temporal knowledge graph architecture for agent memory. Core engine is **Graphiti** — a temporally-aware knowledge graph that synthesizes unstructured conversational data and structured business data while maintaining historical relationships.

Three hierarchical graph tiers: **episode subgraph**, **semantic entity subgraph**, **community subgraph**. Bi-temporal model tracks when events occurred AND when they were ingested; every edge has explicit validity intervals (e.g., "Kendra loves Adidas shoes (as of March 2026)").

- 94.8% on DMR benchmark (vs 93.4% for MemGPT); 18.5% accuracy improvement and 90% latency reduction vs baselines
- ~14,000 GitHub stars; 25,000 weekly PyPI downloads
- [arxiv.org/abs/2501.13956](https://arxiv.org/abs/2501.13956) | [github.com/getzep/graphiti](https://github.com/getzep/graphiti)

#### LangMem SDK (LangChain)

Supports episodic, semantic, and procedural memory types within the LangGraph ecosystem. Provides memory management tools agents use during active conversations plus a background memory manager for automatic extraction/consolidation. Multiple prompt-update algorithms: metaprompt, gradient, prompt_memory.

- [blog.langchain.com/langmem-sdk-launch](https://blog.langchain.com/langmem-sdk-launch/)

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

#### EverMemOS

Self-Organizing Memory Operating System for structured long-horizon reasoning. Engram-inspired memory lifecycle: Episodic Trace Formation segments dialogue into MemCells with episodes, atomic facts, and time-bounded foresight.

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

### Novel Storage: Memvid

Encodes AI memory as video frames using H.264/H.265 codecs; single-file `.mv2` format with no database required. Claims +35% SOTA on LoCoMo, +76% multi-hop, +56% temporal reasoning; 0.025ms P50 retrieval; 1,372x throughput vs standard RAG; 10x compression. Suited for offline, edge, or single-user applications.

- [github.com/memvid/memvid](https://github.com/memvid/memvid)

### Taxonomic Survey: Memory in the Age of AI Agents (December 2025)

Comprehensive taxonomy: **Forms-Functions-Dynamics**.

- **Forms** (storage medium): token-level (explicit, editable), parametric (embedded in weights), latent (continuous vectors / KV-cache)
- **Functions** (purpose): factual, experiential, working memory
- **Dynamics** (lifecycle): Formation → Evolution → Retrieval operators
- Emerging frontiers: memory automation, RL integration, multimodal memory, multi-agent memory, trustworthiness
- [arxiv.org/abs/2512.13564](https://arxiv.org/abs/2512.13564) | [Paper list: github.com/Shichun-Liu/Agent-Memory-Paper-List](https://github.com/Shichun-Liu/Agent-Memory-Paper-List)
- ICLR 2026 accepted a dedicated **MemAgents Workshop** on "Memory for LLM-Based Agentic Systems"

## Emerging Standards and Protocols

### MCP (Model Context Protocol)

Anthropic (November 2024); now governed by Linux Foundation's AAIF. 97 million monthly SDK downloads by February 2026; adopted by Anthropic, OpenAI, Google, Microsoft, Amazon. Defines how agents access external tools, APIs, and data — including memory servers.

### A2A (Agent-to-Agent Protocol)

Google (April 2025); donated to Linux Foundation June 2025. IBM's ACP merged into A2A in August 2025. Standardizes agent discovery, communication, and collaboration.

### AAIF (Agentic AI Foundation)

Linux Foundation; launched December 2025; co-founders: OpenAI, Anthropic, Google, Microsoft, AWS, Block. Governs both MCP and A2A as complementary standards.

### MCP Memory Server Implementations

| Server | Approach | Notable |
|--------|----------|---------|
| **OpenMemory MCP (Mem0)** | Private, local-first memory server | Shared persistent memory layer across MCP-compatible tools |
| **Hindsight** | Structured fact extraction + entity resolution + embeddings | 91.4% on LongMemEval (multi-session: 21.1% → 79.7%) |
| **mcp-memory-service** | Persistent memory with semantic search + web dashboard | Inter-agent messaging bus |
| **agentmemory** | Silent capture, compress, inject for Claude Code / Cursor / Gemini CLI | Passive memory augmentation |
| **MCP Memory Keeper** | SQLite + knowledge graph extraction + semantic search | Progressive compression |

## Benchmarks

| Benchmark | Scope | Details |
|-----------|-------|---------|
| **LoCoMo** | ~1,500-2,000 QA pairs | Single-hop, multi-hop, temporal, open-domain, adversarial; up to 32 sessions / ~600 turns / ~16,000 tokens |
| **LongMemEval** | 500 manually created questions | Information extraction, multi-session reasoning, temporal reasoning, knowledge updates, abstention |
| **DMR** | Dialog memory retrieval | Used by Zep/Graphiti for benchmarking |
| **Terminal-Bench** | Coding agent benchmark | Memory-first agents (Letta Code) lead |

**Caveat:** LoCoMo and LongMemEval were designed for 32k context windows. With million-token context windows now standard, naive "dump everything into context" approaches score competitively, raising questions about whether retrieval-based memory demonstrates clear value on these benchmarks. New benchmarks are needed.

## Key Trends (2025-2026)

1. **Graph-based memory is ascendant** — Zep/Graphiti, Mem0g, MAGMA all use knowledge graphs with temporal awareness, surpassing flat vector stores for relational reasoning
2. **Memory-as-OS abstraction** — MemOS, EverMemOS, and Letta treat memory as a system resource with explicit lifecycle management (MemCube, engrams)
3. **Self-editing vs passive extraction tradeoff** — Letta agents self-edit memory (more adaptive); Mem0 extracts passively (more predictable, token-efficient)
4. **RL-driven memory** — MemRL formalizes memory retrieval as an MDP with Q-value scoring; decouples reasoning from memory evolution
5. **MCP as universal connector** — 97M monthly downloads; every major AI provider adopted; MCP memory servers emerging as standard pattern
6. **Standards consolidation under AAIF** — MCP + A2A + ACP unified under Linux Foundation governance
7. **Compression and forgetting** — entropy-aware filtering, Ebbinghaus decay curves, and progressive consolidation now standard techniques
8. **Multi-agent memory** — computer-architecture-inspired shared/distributed paradigms; A2A enables cross-agent memory sharing
9. **Benchmark limitations** — million-token context windows challenge existing benchmarks; new evaluation frameworks needed

## Strategic Integration

- **The Hub:** Use Zulip's threading or Matrix's persistent rooms to keep L1 (Working) and L2 (Episodic) memory synced between desktop TUI and mobile clients.
- **The Go Logic:** The "Plan, Act, Verify" loop lives at L4 (Procedural), while the OTEL/OTLP stack powers L6 (Observability) and L2 (Episodic) simultaneously.
- **The Moat:** Implementing L7 (Governance) via Open Policy Agent builds an enterprise-ready system that is safer and more reliable than standard implementations.
- **Graph Memory:** Consider Graphiti or Mem0g for L3 (Semantic) to gain temporal awareness and relational reasoning over flat vector retrieval.
- **Memory OS:** Adopt MemCube-style abstractions for uniform memory lifecycle management across layers — versioning, provenance, and composability.
- **MCP Memory Servers:** Expose ycode's memory layers as MCP-compatible endpoints to enable interoperability with the broader agent ecosystem.
- **RL-Driven Retrieval:** Explore MemRL's Q-value approach for L5 (Reflective) to learn which memories are most useful rather than relying solely on semantic similarity.

## References

### Core Infrastructure (L1-L4)

- **L1 (Working Memory):** Zulip Topic-Based Threading for persistent working memory; Matrix Open Standard for room state preservation across clients.
- **L2 (Episodic Memory):** OpenTelemetry (OTEL) Specification for traces and logs that form agent "episodes."
- **L3 (Semantic Memory):** Dgraph and ArcadeDB for Go-native graph and vector storage in structured knowledge bases; Graphiti for temporal knowledge graphs.
- **L4 (Procedural Memory):** Model Context Protocol (MCP) as the standard for representing agent tools and skills to LLMs; now under AAIF governance.

### Architectural Frameworks (L5-L8)

- **SOTA Memory Systems:** Letta (formerly MemGPT), Mem0/Mem0g, Zep/Graphiti, LangMem SDK, MemOS for autonomous memory management.
- **Memory Research:** "Memory in the Age of AI Agents" survey ([arxiv 2512.13564](https://arxiv.org/abs/2512.13564)); MAGMA ([arxiv 2601.03236](https://arxiv.org/abs/2601.03236)); MemRL ([arxiv 2601.03192](https://arxiv.org/abs/2601.03192)); SimpleMem ([github](https://github.com/aiming-lab/SimpleMem)); H-MEM ([arxiv 2507.22925](https://arxiv.org/abs/2507.22925)).
- **L7 (Governance):** Open Policy Agent (OPA) for implementing "Gatekeeper" logic required for safe agentic actions.
- **L8 (Collective Intelligence):** A2A (incorporating ACP) under AAIF; standardized agent discovery and cross-agent memory sharing.

### Standards and Protocols

- **AAIF (Agentic AI Foundation):** Linux Foundation; governs MCP + A2A. Co-founders: OpenAI, Anthropic, Google, Microsoft, AWS, Block.
- **MCP:** [modelcontextprotocol.io](https://modelcontextprotocol.io) — 97M monthly SDK downloads.
- **A2A:** [google A2A announcement](https://developers.googleblog.com/en/a2a-a-new-era-of-agent-interoperability/) — IBM ACP merged August 2025.

### Benchmarks and Evaluation

- **LoCoMo:** [snap-research.github.io/locomo](https://snap-research.github.io/locomo/)
- **LongMemEval:** [arxiv.org/pdf/2410.10813](https://arxiv.org/pdf/2410.10813) (ICLR 2025)
- **ICLR 2026 MemAgents Workshop:** [openreview.net](https://openreview.net/pdf?id=U51WxL382H)

### Theoretical Frontier (L9-L13)

- **L9 (Counterfactuals):** E2B Sandbox runtime for executing "What-If" simulations in isolated environments.
- **L11 (Planck-Scale):** Based on the Holographic Principle (Leonard Susskind, Gerard 't Hooft) regarding information storage on event horizons.
- **L13 (Universal Memory):** Derived from Frank Tipler's Omega Point Theory and Max Tegmark's Mathematical Universe Hypothesis.

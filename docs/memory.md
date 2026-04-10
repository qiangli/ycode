# Memory Management in ycode

ycode implements a multi-layered memory system for AI agent continuity across sessions, projects, and time horizons. This document covers the architecture, mechanisms, design rationale, and the broader state-of-the-art landscape.

## Why Memory Matters

Chat history -- the raw message array sent to the model -- is necessary but insufficient for three reasons:

1. **Context windows are finite.** Even at 200K tokens, a long coding session exhausts capacity within hours. Without compression, the oldest (often most important) context is silently dropped.
2. **Sessions are ephemeral.** When a session ends, everything the agent learned about the user, the project, and its own mistakes vanishes.
3. **Relevance degrades over time.** A memory from last week about a database migration may be critical or irrelevant depending on whether the migration shipped. Raw storage without decay produces "context rot" where irrelevant memories degrade reasoning quality.

The core tension: too much memory causes hallucination noise; too little causes amnesia. A well-designed memory system must be **selective** -- retaining what matters, forgetting what does not, and making the boundary tunable.

---

## Memory Taxonomy

Cognitive science provides useful vocabulary for categorizing agent memory. The mapping is imperfect but helps reason about tradeoffs between capacity, persistence, and retrieval cost.

| Cognitive Type | Definition | ycode Implementation |
| :--- | :--- | :--- |
| **Working** | Active scratchpad, limited capacity | Context window (system prompt + messages) |
| **Episodic** | Specific events, what happened when | Session JSONL, compaction summaries |
| **Semantic** | Facts, concepts, relationships | CLAUDE.md instruction files, persistent project/reference memories |
| **Procedural** | How to do things, learned skills | Skills system, bundled tool definitions |

ycode extends this model with two pragmatic types not found in cognitive science:

- **Contextual memory** -- instruction files (`CLAUDE.md`) discovered by walking the directory ancestry. These encode project conventions that the agent should always know but that change between repositories.
- **Persistent typed memory** -- file-based memories with explicit type tags (`user`, `feedback`, `project`, `reference`) and per-type staleness thresholds. These survive across sessions and are automatically pruned by a background consolidation process.

---

## The Five-Layer Memory Stack

Data flows from ephemeral (top) to persistent (bottom). Each layer has different capacity, lifetime, and retrieval characteristics.

```
┌─────────────────────────────────────────────────┐
│  Layer 1: Working Memory (Context Window)       │  Capacity: ~200K tokens
│  System prompt sections + conversation messages  │  Lifetime: single turn
│  Managed by: prompt/builder.go                  │
├─────────────────────────────────────────────────┤
│  Layer 2: Short-Term (Session JSONL)            │  Capacity: 256KB per file
│  Append-only message log, 3 rotated backups     │  Lifetime: single session
│  Managed by: session/session.go                 │
├─────────────────────────────────────────────────┤
│  Layer 3: Long-Term (Compaction Summaries)      │  Capacity: 1200 chars budget
│  Structured summaries replacing old messages     │  Lifetime: single session
│  Managed by: session/compact.go                 │
├─────────────────────────────────────────────────┤
│  Layer 4: Contextual (Instruction Files)        │  Capacity: 12K chars total
│  CLAUDE.md ancestry chain, deduplicated          │  Lifetime: project lifetime
│  Managed by: prompt/discovery.go                │
├─────────────────────────────────────────────────┤
│  Layer 5: Persistent (File-Based Store)         │  Capacity: 200 index entries
│  Typed markdown files with YAML frontmatter      │  Lifetime: days to 1 year
│  Managed by: memory/store.go, memory/dream.go   │
└─────────────────────────────────────────────────┘
```

### Layer 1: Working Memory (Context Window)

Everything the agent can "think about" in a single turn must fit in the context window. The prompt builder (`internal/runtime/prompt/builder.go`) assembles sections in two groups separated by a cache boundary marker:

- **Static sections** (above boundary): role description, system rules, task guidelines, action guidance. These rarely change between turns.
- **Dynamic sections** (below boundary): environment info, git context, instruction files, recalled memories. These change frequently.

### Layer 2: Short-Term Memory (Session JSONL)

Each session persists messages as newline-delimited JSON in `messages.jsonl`. Implementation: `internal/runtime/session/session.go`.

- **Storage**: `~/.local/share/ycode/sessions/{uuid}/messages.jsonl`
- **Append-on-write**: each `AddMessage()` appends a single JSON line
- **Rotation**: at 256 KB (`MaxSessionFileSize`), old files rotate as `.1`, `.2`, `.3` (max 3 backups)
- **Resume**: `--resume [id|latest]` flag reloads the full message history

### Layer 3: Long-Term Memory (Compaction Summaries)

When estimated token count exceeds 100K (`CompactionThreshold`), the compactor replaces older messages with a structured summary. Implementation: `internal/runtime/session/compact.go`.

- **Trigger**: `NeedsCompaction()` sums `EstimateMessageTokens()` across all messages (rough heuristic: `len(text)/4 + 1` tokens per content block)
- **Boundary rules**: preserves last 4 messages verbatim; never splits tool-use/tool-result pairs
- **Output**: a synthetic system message containing the structured summary
- **Re-compaction**: when a second compaction occurs, `mergeCompactSummaries` folds previous and new summaries together

### Layer 4: Contextual Memory (Instruction Files)

The discovery system (`internal/runtime/prompt/discovery.go`) walks from CWD to the filesystem root searching for:

- `CLAUDE.md`, `CLAUDE.local.md`, `.ycode/CLAUDE.md`, `.ycode/instructions.md`

Files are deduplicated by SHA-256 content hash. Budget: 4,000 chars per file (`MaxFileContentBudget`), 12,000 chars total (`MaxTotalBudget`). Contents are injected into the system prompt below the dynamic boundary.

### Layer 5: Persistent Memory (File-Based Store)

The most durable layer. Implementation: `internal/runtime/memory/store.go`.

- **Storage layout**: `~/.ycode/projects/{hash}/memory/`
- **File format**: Markdown with YAML frontmatter containing `name`, `description`, and `type`
- **Index**: `MEMORY.md` provides a human-readable table of contents (see Section 5.2)
- **Operations**: `Save`, `Load`, `List`, `Delete` via the `Manager` coordinator (`memory/memory.go`)
- **Background maintenance**: the `Dreamer` goroutine prunes stale entries and merges duplicates

---

## Memory Types

Four persistent memory types are defined in `internal/runtime/memory/types.go`. Each type has a different staleness threshold reflecting how quickly that kind of information typically becomes outdated.

| Type | Purpose | Staleness | Example |
| :--- | :--- | :--- | :--- |
| `user` | User preferences, role, working style | 180 days | "User prefers table-driven tests in Go" |
| `feedback` | Corrections, explicit instructions | 1 year | "Always run `make lint` before committing" |
| `project` | Project-specific facts, decisions | 30 days | "Main database is PostgreSQL 16, uses sqlc" |
| `reference` | External resources, documentation pointers | 90 days | "Pipeline bugs tracked in Linear project INGEST" |

Staleness thresholds are defined in `internal/runtime/memory/age.go` (`StalenessThresholds` map). Staleness is checked against `UpdatedAt` (file mtime), not `CreatedAt`. The Dreamer uses `IsStale()` to prune expired memories automatically.

---

## Key Mechanisms

### Relevance Scoring

Implementation: `internal/runtime/memory/search.go`, `scoreMemory()`.

The query string is lowercased and split into tokens. Each token is matched against three fields with weighted scores:

```
Field            Weight
─────────────    ──────
name             +3.0 per matching token
description      +2.0 per matching token
content          +1.0 per matching token
```

After initial scoring, `DecayScore()` (`internal/runtime/memory/age.go`) applies temporal decay:

- **First 7 days**: no decay (score preserved at 100%)
- **After 7 days**: `score * 1.0 / (1.0 + days/30.0)`

This is logarithmic decay -- a 30-day-old memory retains ~50% of its score; a 90-day-old memory retains ~25%. Well-named memories surface even when old because name matches carry 3x weight.

### MEMORY.md Index

Implementation: `internal/runtime/memory/index.go`.

The index is a markdown file with one entry per memory:

```markdown
- [User prefers table tests](user_testing.md) -- user is a Go developer who prefers table-driven tests
- [Auth migration deadline](project_auth.md) -- auth service migration must complete by 2026-04-15
```

- **Cap**: 200 lines (`MaxIndexLines`). Oldest entries are truncated when exceeded.
- **Operations**: `AddEntry` (upsert by filename), `RemoveEntry`, `Read`
- **Purpose**: human-readable table of contents for quick scanning; also loaded into the agent's context at session start without needing to read every memory file.

### Auto-Compaction

Implementation: `internal/runtime/session/compact.go`.

When `EstimateMessageTokens` exceeds 100K across all messages, compaction produces a structured summary with seven fields:

1. **Scope** -- message counts by role (user/assistant/tool)
2. **Tools mentioned** -- deduplicated set of tool names
3. **Recent user requests** -- last 3 requests, 160 chars each
4. **Pending work** -- lines containing "todo", "next", "pending", "follow up", "remaining"
5. **Key files** -- up to 8 file paths identified by common extensions
6. **Current work** -- 200-char excerpt from the most recent non-empty text block
7. **Key timeline** -- role-labeled one-line recap of each compacted message

The summary is injected as a synthetic system message with preamble: *"This session is being continued from a previous conversation..."* and instruction: *"Continue without asking follow-up questions. Resume directly."*

### Summary Compression

Implementation: `internal/runtime/session/compression.go`.

Summaries are compressed to fit within strict budgets:

| Constraint | Value |
| :--- | :--- |
| Max total chars | 1,200 |
| Max lines | 24 |
| Max chars per line | 160 |

Lines are selected by priority tier:

- **P0 (core)**: "Summary:" header, Scope, Current work, Pending work, Key files, Tools
- **P1 (headers)**: section headers (lines ending with `:`)
- **P2 (details)**: bullet points (`- ` or `  - `)
- **P3 (other)**: everything else

Lines are normalized (collapse whitespace), deduplicated (case-insensitive), and truncated. If lines are dropped, an omission notice is appended.

### Background Dreaming / Consolidation

Implementation: `internal/runtime/memory/dream.go`.

The `Dreamer` runs as a background goroutine when `autoDreamEnabled` is true. It executes on a 30-minute interval and performs two operations per cycle:

1. **Stale removal** -- iterates all memories, deletes those past their type-specific staleness threshold via `IsStale()`
2. **Duplicate merging** -- normalizes project memory descriptions (lowercase, collapse whitespace, truncate to 60 chars) and merges entries with identical normalized descriptions, keeping the newer one

The Dreamer is controlled via `context.Context` for graceful shutdown on session exit.

### Auto-Checkpointing

Implementation: `internal/runtime/scratchpad/auto.go`.

When `fileCheckpointingEnabled` is true, compaction events trigger an automatic checkpoint containing:

- Checkpoint ID and label
- Session ID
- Count of compacted messages
- Summary text
- Timestamp

The checkpoint is also appended to the work log for narrative tracking, enabling post-session review of what was compacted and when.

---

## Prompt Cache Optimization

The system prompt is split at `__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__` (`internal/runtime/prompt/boundary.go`):

```
┌───────────────────────────────────┐
│  Static Sections (cacheable)      │
│  ─ Role description               │
│  ─ System rules                   │
│  ─ Task guidelines                │
│  ─ Action guidance                │
├───────────────────────────────────┤
│  __SYSTEM_PROMPT_DYNAMIC_BOUNDARY │
├───────────────────────────────────┤
│  Dynamic Sections (not cached)    │
│  ─ Environment info               │
│  ─ Git context                    │
│  ─ Instruction files (CLAUDE.md)  │
│  ─ Recalled memories              │
└───────────────────────────────────┘
```

Cache fingerprinting (`internal/api/prompt_cache.go`) uses SHA-256 hashes of model, system prompt, tools, and messages. A cache hit occurs when all hashes match within a 5-minute TTL. Cache read tokens cost $0.30/1M vs $3.00/1M for regular input -- a 90% discount.

Memory sections are injected **below** the boundary so that adding, updating, or removing memories does not invalidate the expensive static prompt cache.

---

## State-of-the-Art Landscape

The current SOTA in AI agent memory (2025-2026) has shifted from simple chat history to sophisticated cognitive architectures. The industry now evaluates agents on the LOCOMO benchmark for long-term conversational recall.

### Approaches Comparison

| Approach | Representative | Key Idea | Tradeoff |
| :--- | :--- | :--- | :--- |
| Graph-enhanced memory | Mem0g | Vector store + knowledge graph for relational reasoning | +10% on multi-hop questions; requires external DB infrastructure |
| Raw experience storage | STONE (arXiv 2602.16192) | Store raw experiences, extract on demand | Higher storage cost; avoids "latent information" loss from premature summarization |
| Structural repo maps | Aider | AST-based codebase skeleton always in context | Excellent for refactoring; limited to code structure, not runtime behavior |
| Persistent soul files | OpenClaw | SOUL.md + MEMORY.md + HEARTBEAT.md in human-readable markdown | Simple and inspectable; no query capability or relevance scoring |
| Ultra-large context | Gemini CLI | 1M+ token window, skip RAG entirely | No pruning needed; attention degradation at scale, high cost |
| Checkpoint/rewind | Claude Code | `/rewind` to previous decision point | Transactional safety within a session; does not persist across sessions |
| Recurrent memory | Memory-R1, MemAgent | Agent synthesizes its own memory state before acting | Emerging research; agent "thinks about what to remember" |

### Tool Comparison by Primary Memory Strategy

| Tool | Primary Strategy | Best For |
| :--- | :--- | :--- |
| Cline | Session-task logs with permissioned loop | High-precision, short-term execution tracking |
| Aider | Structural git-maps (repo map) | Deep repository awareness and refactoring |
| OpenClaw | Persistent markdown (stateful soul) | Long-term projects and autonomous monitoring |
| Claude Code | Session snapshots with `/rewind` | Complex debugging and what-if scenarios |
| Gemini CLI | Ultra-large context window (1M+) | Full-repo analysis without RAG |

### Where ycode Fits

ycode is closest to OpenClaw's philosophy -- markdown-first, file-based, human-editable -- but adds structured mechanisms that OpenClaw lacks:

- **Typed staleness thresholds** (30 days to 1 year vs. OpenClaw's manual-only pruning)
- **Relevance scoring with temporal decay** (weighted field matching + logarithmic decay vs. no query capability)
- **Background consolidation** (automatic stale removal and duplicate merging vs. manual maintenance)
- **Structured compaction** (7-field summary with priority-based compression vs. raw truncation)

ycode avoids external databases entirely -- no vector store, no graph DB, no Redis. All state lives in the filesystem, enabling `git` versioning, `grep` searching, and manual editing.

---

## Design Principles

1. **Markdown-first, no database.** All memory is stored as `.md` files with YAML frontmatter. The filesystem is the database. This enables version control with `git`, searching with `grep`, and manual editing with any text editor.

2. **Human-editable.** A user can open `~/.ycode/projects/{hash}/memory/` in any editor, read every memory, correct mistakes, or delete hallucinated entries. MEMORY.md serves as a navigable table of contents. If the agent starts to hallucinate, you can edit its memory to set it straight.

3. **Age-aware with type stratification.** Different memory types decay at different rates. Project facts go stale in 30 days; user preferences persist for 6 months; feedback survives a full year. The Dreamer enforces these thresholds automatically.

4. **Relevance over recency.** The scoring system weights name matches (3x) over content matches (1x). Temporal decay is logarithmic, not linear, so memories degrade slowly. A well-named 60-day-old memory can still outrank a poorly-named 1-day-old one.

5. **Tool-pair integrity.** Compaction never splits a tool-use message from its tool-result. Orphaned tool results confuse the model about what actions were taken and what happened. The compaction boundary walks backward to keep pairs intact.

6. **Cache-friendly prompt structure.** Static prompt content sits above the dynamic boundary; memory, git context, and instruction files sit below. Adding or removing a memory does not invalidate the prompt cache for the expensive static sections, preserving the 90% cache read discount.

---

## File Reference

| Concept | Source File |
| :--- | :--- |
| Memory manager | `internal/runtime/memory/memory.go` |
| Memory types & struct | `internal/runtime/memory/types.go` |
| File-based store | `internal/runtime/memory/store.go` |
| MEMORY.md index | `internal/runtime/memory/index.go` |
| Relevance search & scoring | `internal/runtime/memory/search.go` |
| Age decay & staleness | `internal/runtime/memory/age.go` |
| Background dreaming | `internal/runtime/memory/dream.go` |
| Instruction file discovery | `internal/runtime/memory/discovery.go` |
| Session JSONL persistence | `internal/runtime/session/session.go` |
| Auto-compaction | `internal/runtime/session/compact.go` |
| Summary compression | `internal/runtime/session/compression.go` |
| Prompt builder | `internal/runtime/prompt/builder.go` |
| Prompt file discovery | `internal/runtime/prompt/discovery.go` |
| Dynamic boundary marker | `internal/runtime/prompt/boundary.go` |
| Prompt cache fingerprinting | `internal/api/prompt_cache.go` |
| Auto-checkpointing | `internal/runtime/scratchpad/auto.go` |

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

- `CLAUDE.md`, `CLAUDE.local.md`, `.agents/ycode/CLAUDE.md`, `.agents/ycode/instructions.md`

Files are deduplicated by SHA-256 content hash. Budget: 4,000 chars per file (`MaxFileContentBudget`), 12,000 chars total (`MaxTotalBudget`). Contents are injected into the system prompt below the dynamic boundary.

**`#import` directives**: Instruction files support `#import <relative-path>` directives. When an instruction file contains `#import extra.md`, the referenced file's content is inlined during loading. Circular references are detected and marked with `<!-- circular import: path -->`. Maximum nesting depth is 3. Implementation: `internal/runtime/prompt/import.go`.

**JIT subdirectory discovery**: When tools access files in subdirectories (e.g., `read_file` opens `internal/runtime/memory/store.go`), the JIT discovery system (`internal/runtime/prompt/jit.go`) searches for instruction files from that directory up to the project root. Newly discovered files are merged into the prompt context for the next API turn. This ensures subdirectory-specific instructions are loaded lazily, not just at startup.

### Layer 5: Persistent Memory (File-Based Store)

The most durable layer. Implementation: `internal/runtime/memory/store.go`.

- **Storage layout**: Two tiers -- global (`~/.ycode/memory/`) for cross-project preferences, project (`~/.ycode/projects/{hash}/memory/`) for project-specific facts
- **File format**: Markdown with YAML frontmatter containing `name`, `description`, `type`, and optional `scope`
- **Scoping**: Memories have a `scope` field (`global` or `project`). The Manager queries both stores and merges results, with project-scoped memories ranked higher (1.1x boost). Existing memories without a `scope` field default to `project` for backward compatibility.
- **Index**: `MEMORY.md` provides a human-readable table of contents (see Section 5.2)
- **Operations**: `Save`, `Load`, `List`, `Delete` via the `Manager` coordinator (`memory/memory.go`)
- **Background maintenance**: the `Dreamer` goroutine prunes stale entries and merges duplicates

---

## Three-Layer Context Defense

ycode implements a three-layer defense against context overflow, inspired by OpenClaw's architecture. Each layer is progressively more aggressive. Implementation: `internal/runtime/conversation/runtime.go` (`TurnWithRecovery`).

```
┌───────────────────────────────────────────────────────────────┐
│ Layer 1: Context Pruning                                      │
│ ─ In-memory only (does not modify persisted session)          │
│ ─ Soft trim: truncate old tool results keeping head + tail    │
│ ─ Hard clear: replace old tool results with placeholder       │
│ ─ Trigger: 60% of compaction threshold (60K tokens)           │
│ ─ Managed by: session/pruning.go                              │
├───────────────────────────────────────────────────────────────┤
│ Layer 2: Session Compaction                                   │
│ ─ Generates structured 7-field summary of old messages        │
│ ─ Preserves last 4 messages verbatim                          │
│ ─ Re-injects critical CLAUDE.md sections after compaction     │
│ ─ Trigger: 100% of threshold (100K tokens) or API rejection   │
│ ─ Managed by: session/compact.go, prompt/refresh.go           │
├───────────────────────────────────────────────────────────────┤
│ Layer 3: Emergency Memory Flush                               │
│ ─ Creates minimal continuation: summary + last user message   │
│ ─ Trigger: compaction + retry still fails                     │
│ ─ Managed by: conversation/runtime.go (emergencyFlush)        │
└───────────────────────────────────────────────────────────────┘
```

### Context Health Monitoring

`CheckContextHealth()` in `session/pruning.go` evaluates the current state:

| Level | Threshold | Action |
| :--- | :--- | :--- |
| Healthy | < 60% | No action |
| Warning | 60-80% | Soft/hard trim old tool results |
| Critical | 80-100% | Pruning active, compaction imminent |
| Overflow | > 100% | Proactive compaction before API call |

### Post-Compaction Context Refresh

After compaction removes message history, `PostCompactionRefresh()` in `prompt/refresh.go` re-injects critical sections from CLAUDE.md (Build & Test, Key Design Decisions) into the continuation message. This prevents the agent from losing project-specific operational instructions. Budget: 3,000 chars max.

### History Normalization

Before each API request and on session load, `NormalizeHistory()` (`internal/runtime/session/normalize.go`) enforces the call-output pairing invariant:

- Every `tool_use` block must have a corresponding `tool_result` with matching ID. Missing results (from interrupted tool calls) are synthesized as `[Aborted: tool execution was interrupted]` with `is_error: true`.
- Orphan `tool_result` blocks (no matching `tool_use`) are removed.
- Empty messages left after orphan removal are dropped entirely.

This prevents invalid message sequences from confusing the model or breaking compaction.

### Tool Output Distillation

Large tool outputs are distilled at execution time (`internal/runtime/session/distill.go`) before entering the conversation history:

1. **Exempt tools** (e.g., `read_file`) bypass distillation entirely -- their output IS the point.
2. Outputs under the threshold (default 2,000 chars) pass through unchanged.
3. **Stage 1**: Structural truncation -- keeps first 20 lines + last 10 lines with an omission count.
4. **Stage 2**: Full output is saved to disk (`{sessionDir}/tool_outputs/{tool}_{timestamp}.txt`) so the agent can re-read it if needed. The inline version includes a pointer to the saved file.

This operates at the tool execution layer (not the compaction layer), reducing context pressure from the moment a tool result enters the conversation.

### Ghost Snapshots

Before compaction executes, `SaveGhostSnapshot()` (`internal/runtime/session/ghost.go`) serializes the pre-compaction state to disk:

- Message count, estimated tokens, compaction summary
- UUIDs of compacted messages, key files, active topic
- Stored as `{sessionDir}/ghosts/{timestamp}.json`

Ghost snapshots are never sent to the model. They enable post-mortem analysis and debugging of what was lost during compaction.

### State Snapshots

Unlike compaction summaries which are append-only, the `StateSnapshot` (`internal/runtime/session/state_snapshot.go`) is a cumulative workspace state that is **updated** on each compaction:

```
<state_snapshot>
Goal: implement authentication middleware
Completed:
- designed API schema
- created handler stubs
Current: implement authentication middleware
Files: internal/auth/middleware.go, internal/auth/handler.go
State: tests passing
Compactions: 2
</state_snapshot>
```

The snapshot tracks: primary goal, completed steps (last 10), current step, working files, environment state (e.g., "tests passing", "blocked: compilation error"), and compaction count. Persisted as `{sessionDir}/state_snapshot.json`.

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

When `EstimateMessageTokens` exceeds 100K across all messages, compaction produces a structured intent summary with five explicit categories:

```
<intent_summary>
Scope: 42 messages compacted (user=15, assistant=20, tool=7).
Primary Goal: implement authentication middleware
Verified Facts:
- Tests passing: ok github.com/example/auth
- File modified: edited internal/auth/middleware.go
Working Set: internal/auth/middleware.go, internal/auth/handler.go
Active Blockers:
- bash: FAIL: TestAuthTimeout — context deadline exceeded
Decision Log:
- I'll use the edit approach instead of rewriting the file
Key Files: internal/auth/middleware.go, internal/auth/handler.go, go.mod
Tools Used: read_file, edit_file, bash
Pending Work:
- fix the timeout issue in TestAuthTimeout
</intent_summary>
```

The five categories are extracted by dedicated helpers:

- **`inferPrimaryGoal()`** -- the most recent user request
- **`extractVerifiedFacts()`** -- successful test runs, builds, file modifications
- **`extractWorkingSet()`** -- files explicitly written or edited (not just read)
- **`extractActiveBlockers()`** -- error tool results from recent messages
- **`extractDecisionLog()`** -- assistant messages containing choice language ("I'll use", "chose", "instead of")

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

- **P0 (core)**: "Summary:" header, `Scope:`, `Primary Goal:`, `Verified Facts:`, `Working Set:`, `Active Blockers:`, `Decision Log:`, `Key Files:`, `Tools Used:`, `Pending Work:` (and legacy equivalents)
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

## Prompt Optimization

### Cache-Friendly Prompt Structure

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
│  ─ Active topic (if set)          │
└───────────────────────────────────┘
```

Cache fingerprinting (`internal/api/prompt_cache.go`) uses SHA-256 hashes of model, system prompt, tools, and messages. A cache hit occurs when all hashes match within a 5-minute TTL. Cache read tokens cost $0.30/1M vs $3.00/1M for regular input -- a 90% discount.

Memory sections are injected **below** the boundary so that adding, updating, or removing memories does not invalidate the expensive static prompt cache.

### Differential Context Injection (Non-Caching Providers)

The static/dynamic boundary optimization only benefits providers with explicit prompt caching (Anthropic). For OpenAI-compatible providers (OpenAI, xAI, Ollama, DashScope, etc.), the full system prompt is re-sent every turn.

To address this, ycode implements **differential context injection** (`internal/runtime/prompt/baseline.go`):

1. **Provider capability detection** (`internal/api/capabilities.go`): A static lookup maps `ProviderKind` + model name to capabilities. Anthropic → `CachingSupported: true`; OpenAI/Ollama/unknown → `CachingSupported: false`. Users can override via `providerCapabilities.cachingSupported` in `settings.json`.

2. **Context baseline**: A `ContextBaseline` struct stores SHA-256 hashes of each dynamic section from the previous turn. Before building the system prompt, the builder compares current section contents against the baseline.

3. **Differential mode**: When caching is unavailable and a baseline exists:
   - Only **changed** dynamic sections are included in the prompt
   - Unchanged sections are replaced with `[Context: N section(s) unchanged from previous turn, omitted to save tokens]`
   - On the first turn, after compaction, or after emergency flush, the baseline is reset and all sections are sent

The two strategies coexist: the `CachingSupported` flag routes to the appropriate path in the prompt builder. For local models (Ollama), reducing context size directly reduces inference latency, not just cost.

### Active Topic Tracking

The `TopicTracker` (`internal/runtime/prompt/topic.go`) maintains a lightweight focus signal that helps the model stay on track, especially after compaction:

- **Extraction**: When a user message contains task-changing patterns ("Let's work on...", "help me with...", "please fix...") or is a short directive, the topic is extracted from the first sentence.
- **Follow-up detection**: Messages that look like continuations ("yes", "ok", "thanks", "looks good") do not change the topic.
- **Injection**: If a topic is active, `[Active Topic: {topic}]` is appended to the system prompt as the last dynamic section.
- **Staleness**: The topic is cleared after 20 turns without an update.
- **Recovery**: After compaction, the topic can be restored from the state snapshot.

### Per-File Content Routing

During context pruning, tool results are classified by the routing system (`internal/runtime/session/routing.go`) instead of being treated uniformly:

| Route | Behavior | When Used |
| :--- | :--- | :--- |
| `FULL` | Keep verbatim | Error outputs, write confirmations, small files |
| `PARTIAL` | Head (400 chars) + tail (200 chars) with omission marker | Large file reads, search results, bash output |
| `SUMMARY` | One-line description with line/char counts | Very large bash outputs without diagnostic markers |
| `EXCLUDED` | Placeholder: "Re-run the tool if needed" | Explicitly dropped content |

Routing decisions are cached by content hash to avoid re-classification. Error outputs are always `FULL` regardless of size.

### Startup Prewarming

Session initialization tasks (instruction file discovery, memory loading) are run concurrently using goroutines (`internal/runtime/prompt/prewarm.go`). The `Prewarm()` function returns a `PrewarmResult` containing discovered files and loaded memories once all goroutines complete (or context is cancelled).

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

ycode is closest to OpenClaw's philosophy -- markdown-first, file-based, human-editable -- but incorporates the best ideas from Gemini CLI and Codex:

**From OpenClaw's lineage** (foundational):
- Typed staleness thresholds (30 days to 1 year vs. OpenClaw's manual-only pruning)
- Relevance scoring with temporal decay (weighted field matching + logarithmic decay)
- Background consolidation (automatic stale removal and duplicate merging)

**From Gemini CLI** (adopted):
- JIT subdirectory context loading (discover instruction files when tools access new paths)
- `#import` directive in instruction files (with circular-reference detection)
- Structured intent summary with five explicit categories (Primary Goal, Verified Facts, Working Set, Active Blockers, Decision Log)
- Cumulative state snapshots updated across compactions (not just appended)
- Active topic tracking injected into system prompt
- Hierarchical memory scopes (global + project tiers)
- Per-file content routing for pruning (FULL/PARTIAL/SUMMARY/EXCLUDED)
- Startup prewarming (concurrent initialization)

**From Codex** (adopted):
- Differential context injection for non-caching providers (baseline diffing)
- Provider capability detection (static lookup + config override)
- Ghost snapshots for pre-compaction state recovery
- History normalization with call-output invariant enforcement
- Tool output distillation with disk-backed full output

ycode avoids external databases entirely -- no vector store, no graph DB, no Redis. All state lives in the filesystem, enabling `git` versioning, `grep` searching, and manual editing.

---

## Design Principles

1. **Markdown-first, no database.** All memory is stored as `.md` files with YAML frontmatter. The filesystem is the database. This enables version control with `git`, searching with `grep`, and manual editing with any text editor.

2. **Human-editable.** A user can open `~/.ycode/projects/{hash}/memory/` in any editor, read every memory, correct mistakes, or delete hallucinated entries. MEMORY.md serves as a navigable table of contents. If the agent starts to hallucinate, you can edit its memory to set it straight.

3. **Age-aware with type stratification.** Different memory types decay at different rates. Project facts go stale in 30 days; user preferences persist for 6 months; feedback survives a full year. The Dreamer enforces these thresholds automatically.

4. **Relevance over recency.** The scoring system weights name matches (3x) over content matches (1x). Temporal decay is logarithmic, not linear, so memories degrade slowly. A well-named 60-day-old memory can still outrank a poorly-named 1-day-old one.

5. **Tool-pair integrity.** Compaction never splits a tool-use message from its tool-result. Orphaned tool results confuse the model about what actions were taken and what happened. The compaction boundary walks backward to keep pairs intact.

6. **Cache-friendly prompt structure.** Static prompt content sits above the dynamic boundary; memory, git context, and instruction files sit below. Adding or removing a memory does not invalidate the prompt cache for the expensive static sections, preserving the 90% cache read discount.

7. **Provider-adaptive optimization.** Caching providers (Anthropic) use the static/dynamic boundary. Non-caching providers (OpenAI, Ollama) use differential context injection. Capability detection is automatic with user override.

8. **Lazy context enrichment.** Instruction files are discovered at startup from CWD ancestry, then dynamically expanded via JIT discovery as tools access new subdirectories. The `#import` directive allows factoring instructions across files without manual concatenation.

9. **Cumulative state, not append-only history.** State snapshots represent the integrated workspace state and are updated (not appended) on each compaction. Ghost snapshots preserve the pre-compaction state for debugging. Together they provide both a forward-looking view (state snapshot) and a backward-looking record (ghost snapshot).

10. **History invariants.** Every tool_use must have a tool_result. Normalization enforces this on session load and before API calls. Tool output distillation operates at the execution layer, not the compaction layer, reducing context pressure early.

---

## File Reference

| Concept | Source File |
| :--- | :--- |
| **Memory system** | |
| Memory manager (dual-scope) | `internal/runtime/memory/memory.go` |
| Memory types, Scope | `internal/runtime/memory/types.go` |
| File-based store | `internal/runtime/memory/store.go` |
| MEMORY.md index | `internal/runtime/memory/index.go` |
| Relevance search & scoring | `internal/runtime/memory/search.go` |
| Age decay & staleness | `internal/runtime/memory/age.go` |
| Background dreaming | `internal/runtime/memory/dream.go` |
| Instruction file discovery | `internal/runtime/memory/discovery.go` |
| **Session management** | |
| Session JSONL persistence | `internal/runtime/session/session.go` |
| Auto-compaction (intent summary) | `internal/runtime/session/compact.go` |
| Summary compression | `internal/runtime/session/compression.go` |
| History normalization | `internal/runtime/session/normalize.go` |
| Tool output distillation | `internal/runtime/session/distill.go` |
| Per-file content routing | `internal/runtime/session/routing.go` |
| Ghost snapshots | `internal/runtime/session/ghost.go` |
| State snapshots | `internal/runtime/session/state_snapshot.go` |
| Context pruning (Layer 1) | `internal/runtime/session/pruning.go` |
| LLM-based summarization | `internal/runtime/session/llm_summary.go` |
| **Prompt assembly** | |
| Prompt builder (differential mode) | `internal/runtime/prompt/builder.go` |
| Prompt file discovery | `internal/runtime/prompt/discovery.go` |
| JIT subdirectory discovery | `internal/runtime/prompt/jit.go` |
| #import directive | `internal/runtime/prompt/import.go` |
| Active topic tracking | `internal/runtime/prompt/topic.go` |
| Context baseline (differential) | `internal/runtime/prompt/baseline.go` |
| Startup prewarming | `internal/runtime/prompt/prewarm.go` |
| Dynamic boundary marker | `internal/runtime/prompt/boundary.go` |
| Post-compaction refresh | `internal/runtime/prompt/refresh.go` |
| **API & providers** | |
| Prompt cache fingerprinting | `internal/api/prompt_cache.go` |
| Provider capability detection | `internal/api/capabilities.go` |
| **Runtime** | |
| 3-layer defense (TurnWithRecovery) | `internal/runtime/conversation/runtime.go` |
| On-demand compaction (CompactNow) | `internal/runtime/conversation/runtime.go` |
| Auto-checkpointing | `internal/runtime/scratchpad/auto.go` |
| **Tools** | |
| Agent-requested condensation | `internal/tools/compact_context.go` |

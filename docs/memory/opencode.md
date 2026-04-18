# Memory Management Summary: priorart/opencode/

This document summarizes how the ycode/priorart/opencode/ system handles **multi-turn conversations**, **user/system prompts**, **context engineering**, and **harness engineering** with respect to memory management.

---

## 1. Multi-Turn Handling

### Session Persistence (Layer 2: Short-Term Memory)
- **Format**: Each session stores messages as newline-delimited JSON (`messages.jsonl`)
- **Storage**: `~/.local/share/ycode/sessions/{uuid}/messages.jsonl`
- **Rotation**: At 256KB, files rotate with max 3 backups (`.1`, `.2`, `.3`)
- **Resume**: Sessions can be resumed via `--resume [id|latest]` flag

### Auto-Compaction (Layer 3: Long-Term Memory)
When token count exceeds 100K (`CompactionThreshold`):
- **Trigger**: `NeedsCompaction()` estimates tokens via `len(text)/4 + 1` heuristic
- **Boundary Rules**: Preserves last 4 messages verbatim; never splits tool-use/tool-result pairs
- **Output**: A synthetic system message with 7-field structured summary:
  1. **Scope** - message counts by role
  2. **Tools mentioned** - deduplicated tool names
  3. **Recent user requests** - last 3 requests (160 chars each)
  4. **Pending work** - todo/next/pending/follow up lines
  5. **Key files** - up to 8 file paths by common extensions
  6. **Current work** - 200-char excerpt from most recent text block
  7. **Key timeline** - role-labeled one-line recap

### Summary Compression
Structured summaries fit within strict budgets:
| Constraint | Value |
|------------|-------|
| Max chars | 1,200 |
| Max lines | 24 |
| Max chars/line | 160 |

**Priority tiers**: P0 (core: scope, current work, pending, files, tools) → P1 (headers) → P2 (bullet points) → P3 (other)

### Tool-Pair Integrity
Critical constraint: compaction **never splits** a `tool-use` message from its `tool-result`. The boundary walks backward to keep pairs intact, preventing orphaned results that would confuse the model.

---

## 2. User/System Prompts

### The Five-Layer Memory Stack

```
┌─────────────────────────────────────────────────┐
│  Layer 1: Working Memory (Context Window)       │  ~200K tokens
│  System prompt sections + conversation messages  │  Single turn
│  Managed by: prompt/builder.go                  │
├─────────────────────────────────────────────────┤
│  Layer 2: Short-Term (Session JSONL)            │  256KB/file
│  Append-only message log, 3 rotated backups     │  Single session
│  Managed by: session/session.go                 │
├─────────────────────────────────────────────────┤
│  Layer 3: Long-Term (Compaction Summaries)      │  1200 chars
│  Structured summaries replacing old messages    │  Single session
│  Managed by: session/compact.go                 │
├─────────────────────────────────────────────────┤
│  Layer 4: Contextual (Instruction Files)        │  12K chars total
│  CLAUDE.md ancestry chain, deduplicated         │  Project lifetime
│  Managed by: prompt/discovery.go                │
├─────────────────────────────────────────────────┤
│  Layer 5: Persistent (File-Based Store)         │  200 index entries
│  Typed markdown files with YAML frontmatter     │  Days to 1 year
│  Managed by: memory/store.go, memory/dream.go   │
└─────────────────────────────────────────────────┘
```

### Working Memory Structure (Layer 1)

The prompt builder assembles sections in two groups separated by a **cache boundary**:

**Static sections** (above boundary - cacheable):
- Role description
- System rules
- Task guidelines
- Action guidance

**Dynamic sections** (below boundary - not cached):
- Environment info
- Git context
- Instruction files (CLAUDE.md)
- Recalled memories

### Contextual Memory (Layer 4): Instruction Files

The discovery system walks from CWD to filesystem root searching for:
- `CLAUDE.md`, `CLAUDE.local.md`, `.agents/ycode/CLAUDE.md`, `.agents/ycode/instructions.md`

**Budget constraints**:
- 4,000 chars per file (`MaxFileContentBudget`)
- 12,000 chars total (`MaxTotalBudget`)
- Files deduplicated by SHA-256 content hash

### Persistent Memory Types (Layer 5)

Four typed memories with staleness thresholds:

| Type | Purpose | Staleness | Example |
|------|---------|-----------|---------|
| `user` | User preferences, working style | 180 days | "Prefers table-driven tests in Go" |
| `feedback` | Corrections, explicit instructions | 1 year | "Always run `make lint` before committing" |
| `project` | Project-specific facts, decisions | 30 days | "Main database is PostgreSQL 16, uses sqlc" |
| `reference` | External resources, documentation | 90 days | "Bugs tracked in Linear project INGEST" |

---

## 3. Context Engineering

### Prompt Cache Optimization

The system prompt uses a dynamic boundary marker (`__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__`):

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

**Cache mechanics** (`internal/api/prompt_cache.go`):
- Fingerprinting via SHA-256 hashes of model, system prompt, tools, messages
- Cache hit: all hashes match within 5-minute TTL
- Cost: $0.30/1M cache read tokens vs $3.00/1M regular input (**90% discount**)
- Memory sections injected **below** boundary so adding/removing memories doesn't invalidate static cache

### Relevance Scoring & Retrieval

**Query processing** (`internal/runtime/memory/search.go`):
- Query lowercased and tokenized
- Weighted field matching:
  - `name`: +3.0 per matching token
  - `description`: +2.0 per matching token
  - `content`: +1.0 per matching token

**Temporal Decay** (`internal/runtime/memory/age.go`):
- First 7 days: no decay (100% score preserved)
- After 7 days: `score * 1.0 / (1.0 + days/30.0)`
- Logarithmic decay: 30-day-old = ~50%, 90-day-old = ~25%

Well-named memories surface even when old (name matches carry 3x weight).

### MEMORY.md Index

Human-readable table of contents at `~/.ycode/projects/{hash}/memory/MEMORY.md`:
- Cap: 200 lines (`MaxIndexLines`)
- Oldest entries truncated when exceeded
- Loaded into agent context at session start without reading every memory file

---

## 4. Harness Engineering

### Background Dreaming / Consolidation

The `Dreamer` goroutine runs when `autoDreamEnabled` is true (`internal/runtime/memory/dream.go`):
- **Interval**: 30 minutes
- **Operations per cycle**:
  1. **Stale removal**: Delete memories past type-specific staleness threshold
  2. **Duplicate merging**: Normalize descriptions (lowercase, collapse whitespace, truncate 60 chars), merge identical entries keeping newer
- **Control**: Managed via `context.Context` for graceful shutdown

### Auto-Checkpointing

When `fileCheckpointingEnabled` is true (`internal/runtime/scratchpad/auto.go`):
- Compaction events trigger automatic checkpoints
- Checkpoint contains: ID, label, session ID, compacted message count, summary text, timestamp
- Appended to work log for narrative tracking

### Design Principles

| Principle | Implementation |
|-----------|----------------|
| **Markdown-first, no database** | All memory as `.md` files with YAML frontmatter; filesystem is the database |
| **Human-editable** | User can open memory folder, read/correct/delete entries; MEMORY.md as navigable TOC |
| **Age-aware with type stratification** | Different decay rates: project (30d), reference (90d), user (180d), feedback (1y) |
| **Relevance over recency** | Name matches 3x weight; logarithmic decay preserves old but well-named memories |
| **Tool-pair integrity** | Compaction never splits tool-use from tool-result |
| **Cache-friendly prompt structure** | Static above boundary, dynamic below; memory changes don't invalidate cache |

### Cognitive Science Mapping

| Cognitive Type | ycode Implementation |
|----------------|---------------------|
| **Working** | Context window (system prompt + messages) |
| **Episodic** | Session JSONL, compaction summaries |
| **Semantic** | CLAUDE.md instruction files, persistent project/reference memories |
| **Procedural** | Skills system, bundled tool definitions |

Plus two pragmatic extensions:
- **Contextual memory**: CLAUDE.md ancestry chain
- **Persistent typed memory**: File-based with staleness thresholds and background consolidation

---

## File Reference

| Component | Source File |
|-----------|-------------|
| Memory manager | `internal/runtime/memory/memory.go` |
| Memory types & struct | `internal/runtime/memory/types.go` |
| File-based store | `internal/runtime/memory/store.go` |
| MEMORY.md index | `internal/runtime/memory/index.go` |
| Relevance search & scoring | `internal/runtime/memory/search.go` |
| Age decay & staleness | `internal/runtime/memory/age.go` |
| Background dreaming | `internal/runtime/memory/dream.go` |
| Instruction file discovery | `internal/runtime/prompt/discovery.go` |
| Session JSONL persistence | `internal/runtime/session/session.go` |
| Auto-compaction | `internal/runtime/session/compact.go` |
| Summary compression | `internal/runtime/session/compression.go` |
| Prompt builder | `internal/runtime/prompt/builder.go` |
| Dynamic boundary marker | `internal/runtime/prompt/boundary.go` |
| Prompt cache fingerprinting | `internal/api/prompt_cache.go` |
| Auto-checkpointing | `internal/runtime/scratchpad/auto.go` |

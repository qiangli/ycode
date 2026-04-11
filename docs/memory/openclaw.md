# OpenClaw Memory Management: Multi-Turn, Prompt Engineering, and Harness Design

This document analyzes how OpenClaw handles multi-turn conversations, prompt/context engineering, and harness engineering with respect to memory management. OpenClaw represents a sophisticated approach to AI agent memory that influenced ycode's design.

---

## 1. Architecture Overview

OpenClaw implements a **persistent markdown-first memory system** centered around human-editable files in the workspace. Unlike systems that rely on vector databases or external stores, OpenClaw uses the filesystem as its primary memory layer.

### Core Philosophy

| Principle | Implementation |
|----------|----------------|
| **Human-editable** | All memory is `.md` files with YAML frontmatter |
| **Filesystem as database** | No external dependencies; `git` for versioning |
| **Session continuity** | JSONL-based session files with tree structure |
| **Prompt-centric** | Memory surfaces through structured system prompt composition |

---

## 2. Multi-Turn Conversation Handling

### 2.1 Session File Structure

OpenClaw uses **JSONL (newline-delimited JSON)** session files with a tree-based structure supporting branching:

```
session.jsonl
├── header (session metadata)
├── entries[]
│   ├── message (user/assistant/tool)
│   ├── compaction (summarization markers)
│   ├── custom (application-defined events)
│   ├── model_change, thinking_level_change
│   └── label, branch_summary (metadata)
```

Key files:
- `src/agents/pi-embedded-runner/session-truncation.ts` - Post-compaction file truncation
- `src/agents/pi-embedded-runner/history.ts` - History limiting by turn count
- `src/agents/session-transcript-repair.ts` - Tool-use/tool-result pairing repair

### 2.2 History Management Strategies

**Turn-Based Limiting** (`limitHistoryTurns`):
```typescript
// Limit to last N user turns (and their assistant responses)
function limitHistoryTurns(messages: AgentMessage[], limit: number): AgentMessage[]
```

Per-session-type limits:
- **DM sessions**: `dmHistoryLimit` with per-user overrides
- **Channel/group sessions**: `historyLimit` from provider config
- **Subagent/cron sessions**: Minimal bootstrap allowlist only

**Tool-Result Pair Integrity**:
- Never splits `tool_use` from `tool_result` messages
- Repairs orphaned pairs after truncation/compaction
- Validates pairings before sending to model

### 2.3 Session Truncation After Compaction

After compaction summarizes old messages, OpenClaw **physically truncates** the session file:

```typescript
// src/agents/pi-embedded-runner/session-truncation.ts
export async function truncateSessionAfterCompaction(params: {
  sessionFile: string;
  ackMaxChars?: number;
  heartbeatPrompt?: string;
}): Promise<TruncationResult>
```

Keeps:
1. Session header
2. Non-message state (custom events, model changes)
3. Unsummarized tail (from `firstKeptEntryId` onward)
4. Sibling branches not covered by compaction

Removes:
- Summarized `message` entries before `firstKeptEntryId`
- Heartbeat ping/pong pairs (user message + HEARTBEAT_OK response)
- Dangling label/branch_summary metadata

---

## 3. Prompt & Context Engineering

### 3.1 The Bootstrap Context Files

OpenClaw defines a canonical set of workspace markdown files that compose the agent's context:

| File | Purpose | Priority |
|------|---------|----------|
| `AGENTS.md` | Agent instructions, critical rules | Highest |
| `SOUL.md` | Persona, tone, character definition | High |
| `IDENTITY.md` | Agent self-identity | High |
| `USER.md` | User preferences, working style | High |
| `TOOLS.md` | Tool usage guidance | Medium |
| `MEMORY.md` | Persistent memory index | Medium |
| `HEARTBEAT.md` | Periodic polling configuration | Dynamic |
| `BOOTSTRAP.md` | Initialization checklist | Medium |

Key file: `src/agents/workspace.ts`

### 3.2 Context File Ordering

```typescript
// src/agents/system-prompt.ts
const CONTEXT_FILE_ORDER = new Map<string, number>([
  ["agents.md", 10],
  ["soul.md", 20],
  ["identity.md", 30],
  ["user.md", 40],
  ["tools.md", 50],
  ["bootstrap.md", 60],
  ["memory.md", 70],
]);
```

Files are sorted by priority, with dynamic files (like `HEARTBEAT.md`) placed below the cache boundary.

### 3.3 The Cache Boundary Pattern

OpenClaw implements a **two-tier prompt structure** for cache optimization:

```
┌─────────────────────────────────────┐
│  Static Sections (cacheable)        │
│  ─ Role description                 │
│  ─ System rules                     │
│  ─ Tool definitions                 │
│  ─ Skills guidance                  │
│  ─ Safety constraints               │
├─────────────────────────────────────┤
│  <!-- OPENCLAW_CACHE_BOUNDARY -->   │  ← Cache invalidation point
├─────────────────────────────────────┤
│  Dynamic Sections (not cached)      │
│  ─ HEARTBEAT.md                     │
│  ─ Group chat context               │
│  ─ Runtime info (time, model, etc.) │
└─────────────────────────────────────┘
```

Implementation: `src/agents/system-prompt-cache-boundary.ts`

This allows Anthropic-style prompt caching to reuse the expensive static portion (~90% cost reduction) while keeping frequently-changing context below the boundary.

### 3.4 Prompt Modes

OpenClaw supports three prompt modes for different agent types:

| Mode | Use Case | Sections Included |
|------|----------|-------------------|
| `full` | Main agent | All sections |
| `minimal` | Subagents | Tooling, Workspace, Runtime only |
| `none` | Special cases | Basic identity only |

Subagents and cron jobs use `minimal` mode to reduce token consumption.

---

## 4. Harness Engineering: The Compaction System

### 4.1 Three-Layer Memory Defense

OpenClaw implements a three-layer approach to context window management:

```
┌─────────────────────────────────────────────────────────────┐
│ Layer 1: Context Pruning ("microcompact")                   │
│ ─ In-memory only                                            │
│ ─ Soft trim: truncate tool results (head/tail preservation) │
│ ─ Hard clear: replace with placeholders                     │
│ ─ Trigger: configurable ratio of context window             │
├─────────────────────────────────────────────────────────────┤
│ Layer 2: Session Compaction ("compact")                     │
│ ─ Generates LLM summary of old messages                     │
│ ─ Persists compaction marker in session file                │
│ ─ Keeps last N messages verbatim                            │
│ ─ Trigger: token budget threshold                           │
├─────────────────────────────────────────────────────────────┤
│ Layer 3: Memory Flush ("flush")                             │
│ ─ Creates new session with summary as context               │
│ ─ Writes compaction to MEMORY.md                            │
│ ─ Updates session tracking                                  │
│ ─ Trigger: emergency threshold approaching                  │
└─────────────────────────────────────────────────────────────┘
```

### 4.2 Context Pruning (Microcompact)

File: `src/agents/pi-hooks/context-pruning/pruner.ts`

Operates on in-memory messages before sending to the model:

```typescript
export function pruneContextMessages(params: {
  messages: AgentMessage[];
  settings: EffectiveContextPruningSettings;
  ctx: Pick<ExtensionContext, "model">;
}): AgentMessage[]
```

**Soft Trim** (ratio-based):
- Truncates tool results keeping head and tail
- Preserves structure with `[...]` markers
- Configurable `headChars` and `tailChars`

**Hard Clear** (emergency):
- Replaces entire tool result with placeholder
- Only for prunable tools (configured allowlist)
- Triggered at higher ratio threshold

**Safety**: Never prunes before first user message (protects bootstrap context reads).

### 4.3 Session Compaction

File: `src/agents/pi-embedded-runner/compact.ts`

Full compaction uses an LLM to generate structured summaries:

```typescript
export async function compactEmbeddedPiSession(
  params: CompactEmbeddedPiSessionParams
): Promise<EmbeddedPiCompactResult>
```

**Process**:
1. Load session and validate tool-result pairings
2. Apply history limiting (if configured)
3. Check for real conversation content (skip if only heartbeats)
4. Call `session.compact()` from pi-coding-agent SDK
5. Persist compaction checkpoint
6. Run post-compaction hooks
7. Optionally truncate session file

**Summary Structure**:
- Scope (message counts by role)
- Tools mentioned (deduplicated)
- Recent user requests (last 3)
- Pending work (todo/next/follow up)
- Key files (up to 8 by extension)
- Current work (excerpt from latest message)
- Key timeline (one-line recap)

**Post-Compaction Context Refresh**:

File: `src/auto-reply/reply/post-compaction-context.ts`

After compaction, critical sections from `AGENTS.md` are re-injected:

```typescript
export async function readPostCompactionContext(
  workspaceDir: string,
  cfg?: OpenClawConfig,
): Promise<string | null>
```

Default sections: `["Session Startup", "Red Lines"]`
Legacy fallback: `["Every Session", "Safety"]`

This ensures the agent re-reads critical instructions after losing detailed history.

### 4.4 Memory Flush

File: `src/auto-reply/reply/agent-runner-memory.ts`

When approaching hard limits, OpenClaw performs a **memory flush**:

```typescript
export async function runMemoryFlushIfNeeded(params: {
  cfg: OpenClawConfig;
  followupRun: FollowupRun;
  // ...
}): Promise<SessionEntry | undefined>
```

**Flush Process**:
1. Spawn subagent with `memoryFlushWritePath` directive
2. Subagent summarizes session to `MEMORY.md`
3. Create new session with summary as bootstrap context
4. Update session tracking (sessionId, sessionFile)
5. Refresh followup queue with new session

**Flush Plan Configuration**:
```typescript
type MemoryFlushPlan = {
  softThresholdTokens: number;   // Start considering flush
  forceFlushTranscriptBytes: number;  // Emergency size limit
  reserveTokensFloor: number;    // Minimum tokens to reserve
  prompt: string;                // Flush subagent prompt
  systemPrompt: string;          // Additional system prompt
  relativePath: string;          // Where to write (e.g., "memory/flush.md")
}
```

### 4.5 Preflight Compaction

Before each agent run, OpenClaw checks if compaction is needed:

```typescript
export async function runPreflightCompactionIfNeeded(params: {
  cfg: OpenClawConfig;
  followupRun: FollowupRun;
  // ...
}): Promise<SessionEntry | undefined>
```

Uses token estimates from:
- Persisted session metadata (`totalTokens`)
- Transcript analysis (last non-zero usage)
- Current prompt estimation

**Gating conditions**:
- Skip for heartbeats
- Skip for CLI providers
- Skip if tokens are fresh and below threshold

---

## 5. Persistent Memory System

### 5.1 Vector + FTS Hybrid Search

OpenClaw implements a sophisticated memory retrieval system using SQLite with both vector and full-text search:

File: `src/agents/memory-search.ts`

```typescript
export type ResolvedMemorySearchConfig = {
  enabled: boolean;
  sources: Array<"memory" | "sessions">;
  provider: string;        // Embedding provider
  store: {
    driver: "sqlite";
    path: string;
    fts: { tokenizer: "unicode61" | "trigram" };
    vector: { enabled: boolean; extensionPath?: string };
  };
  chunking: { tokens: number; overlap: number };
  query: {
    maxResults: number;
    minScore: number;
    hybrid: {
      enabled: boolean;
      vectorWeight: number;      // Default: 0.7
      textWeight: number;        // Default: 0.3
      mmr: { enabled: boolean; lambda: number };  // Maximal Marginal Relevance
      temporalDecay: { enabled: boolean; halfLifeDays: number };
    };
  };
}
```

**Hybrid Scoring**:
```
final_score = (vector_score * vectorWeight) + (text_score * textWeight)
```

With optional temporal decay:
```
decayed_score = score * (0.5 ^ (days_ago / halfLifeDays))
```

### 5.2 Memory Synchronization

Configuration-driven sync behavior:

```typescript
sync: {
  onSessionStart: boolean;   // Sync at session start
  onSearch: boolean;         // Sync before search
  watch: boolean;            // File watching
  watchDebounceMs: number;   // 1500ms default
  intervalMinutes: number;   // Periodic sync
  sessions: {
    deltaBytes: number;      // Sync after 100KB new data
    deltaMessages: number;   // Sync after 50 new messages
    postCompactionForce: boolean;  // Force sync after compaction
  };
}
```

### 5.3 Memory Prompt Integration

Memory surfaces in the system prompt through a plugin-based architecture:

File: `src/plugins/memory-state.ts`

```typescript
export type MemoryPromptSectionBuilder = (params: {
  availableTools: Set<string>;
  citationsMode?: MemoryCitationsMode;
}) => string[];
```

The memory section includes:
- Available memory tools (`memory_search`, `memory_save`, etc.)
- Citation format instructions
- Recall guidance (when to search vs. rely on context)

---

## 6. Session State Management

### 6.1 Session Store

OpenClaw maintains a session store for tracking conversation metadata:

```typescript
type SessionEntry = {
  sessionId: string;
  sessionFile?: string;
  totalTokens?: number;
  totalTokensFresh?: boolean;
  compactionCount?: number;
  memoryFlushAt?: number;
  memoryFlushCompactionCount?: number;
  skillsSnapshot?: SkillSnapshot;
  groupId?: string;
  groupChannel?: string;
  groupSpace?: string;
}
```

Storage: JSON file in `~/.openclaw/sessions/store.json`

### 6.2 Write Locking

Session files use cooperative write locking:

```typescript
export async function acquireSessionWriteLock(params: {
  sessionFile: string;
  maxHoldMs: number;
}): Promise<{ release(): Promise<void> }>
```

Prevents corruption during concurrent access (compaction + new messages).

---

## 7. Comparison with ycode

| Feature | OpenClaw | ycode |
|---------|----------|-------|
| **Memory storage** | SQLite + FTS + Vector | Filesystem (.md files) |
| **Session format** | JSONL with tree structure | JSONL (linear) |
| **Bootstrap files** | 8 canonical files | CLAUDE.md ancestry |
| **Compaction** | LLM-generated summaries | 7-field structured summary |
| **Context pruning** | Soft trim + hard clear | Similar with priority tiers |
| **Memory flush** | Subagent-based | Dreamer background task |
| **Relevance scoring** | Hybrid vector+FTS + MMR | Token-based field weighting |
| **Staleness** | Temporal decay | Type-specific thresholds |
| **Cache boundary** | Static/dynamic split | Similar implementation |

### Key Differences

1. **Search Architecture**: OpenClaw uses SQLite with vector extensions; ycode uses file-based search with weighted token matching.

2. **Memory Types**: ycode has explicit typed memories (user/feedback/project/reference) with different staleness thresholds; OpenClaw uses a unified system with temporal decay.

3. **Consolidation**: ycode's Dreamer runs background consolidation; OpenClaw relies on explicit compaction and flush operations.

4. **Human Editability**: Both prioritize markdown, but ycode's MEMORY.md is strictly an index; OpenClaw's memory directory contains chunked/embeddable content.

---

## 8. File Reference

| Concept | OpenClaw Source File |
|---------|---------------------|
| Session compaction | `src/agents/pi-embedded-runner/compact.ts` |
| Session truncation | `src/agents/pi-embedded-runner/session-truncation.ts` |
| History limiting | `src/agents/pi-embedded-runner/history.ts` |
| Context pruning | `src/agents/pi-hooks/context-pruning/pruner.ts` |
| System prompt | `src/agents/system-prompt.ts` |
| Bootstrap files | `src/agents/bootstrap-files.ts` |
| Workspace files | `src/agents/workspace.ts` |
| Memory search config | `src/agents/memory-search.ts` |
| Memory state plugin | `src/plugins/memory-state.ts` |
| Memory flush | `src/auto-reply/reply/agent-runner-memory.ts` |
| Post-compaction context | `src/auto-reply/reply/post-compaction-context.ts` |
| Session repair | `src/agents/session-transcript-repair.ts` |

---

## 9. Key Design Insights

### 9.1 The "Stateful Soul" Pattern

OpenClaw's SOUL.md represents a **persistent persona** that survives across sessions. Unlike ephemeral system prompts, the soul file:
- Is always loaded from disk (fresh each session)
- Can be edited by the user at any time
- Provides continuity of personality even after memory flush

### 9.2 Compaction as Conversation Surgery

OpenClaw treats compaction as **precision surgery** rather than crude truncation:
- Preserves last N assistant turns verbatim
- Never breaks tool-use/tool-result pairs
- Generates meaningful LLM summaries
- Maintains tree structure for branching

### 9.3 The Three-Phase Defense

The layered approach (prune → compact → flush) provides graceful degradation:
1. **Pruning** is fast and lossy (tool results only)
2. **Compaction** is slower but preserves semantics
3. **Flush** is the nuclear option (new session)

### 9.4 Cache-Aware Prompt Engineering

The static/dynamic boundary recognizes that:
- Static content is expensive to generate but changes rarely
- Dynamic content is cheap but changes every turn
- Anthropic's prompt caching provides 90% cost reduction when respected

---

*This analysis is based on the OpenClaw codebase as of April 2025.*

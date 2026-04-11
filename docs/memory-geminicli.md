# Gemini CLI Memory Management: Multi-Turn, Prompt Engineering, and Harness Design

This document analyzes how Gemini CLI handles multi-turn conversations, prompt/context engineering, and harness engineering with respect to memory management. Gemini CLI represents Google's approach to AI coding agents and introduces several novel context management techniques.

---

## 1. Architecture Overview

Gemini CLI implements a **4-tier hierarchical memory system** with aggressive context compression and JIT (Just-In-Time) context loading. The system is built in TypeScript and designed for Google's Gemini models with 1M+ token context windows.

### Core Philosophy

| Principle | Implementation |
|----------|----------------|
| **Hierarchical scoping** | Global → Extension → Project → Subdirectory memory tiers |
| **JIT context loading** | Instruction files discovered lazily when tools access paths |
| **Multi-phase compression** | 4-phase compression with self-correcting verification |
| **Per-file routing** | Individual content routing decisions (FULL/PARTIAL/SUMMARY/EXCLUDED) |

Key file: `packages/core/src/context/memoryContextManager.ts`

---

## 2. Multi-Turn Conversation Handling

### 2.1 Agent History Provider

File: `packages/core/src/context/agentHistoryProvider.ts`

Gemini CLI implements **state snapshot-based continuity** with multi-stage truncation:

**Stage 1 — Message Normalization**: Enforce size limits differentiated by recency.
- Recent messages: `maximumMessageTokens` (high limit, ~8000 tokens)
- Older messages: `normalMessageTokens` (lower limit, ~4000 tokens)
- Grace zone: Last N accumulated tokens fully protected

**Stage 2 — Boundary Detection**: Backward scan from most recent message to identify the keep/truncate split point. Respects `retainedTokens` budget and preserves `functionCall`/`functionResponse` pairs.

**Stage 3 — Summarization**: Generates an intent summary for truncated history via a secondary model call:
```xml
<intent_summary>
- **Primary Goal**: User's ultimate objective
- **Verified Facts**: Definitively completed items
- **Working Set**: Current files under analysis
- **Active Blockers**: Exact error messages/failing tests
</intent_summary>
```
Falls back to a structured text summary on model call failure.

**Stage 4 — Merge with Grace Zone**: Reintegrates the summary with retained messages. Ensures user/model role alternation. Prepends summary to first retained message if it's from the user.

### 2.2 Chat Compression Service

File: `packages/core/src/context/chatCompressionService.ts`

Advanced multi-phase compression pipeline:

**Phase 1 — Reverse Token Budget Truncation**: Scans history backward (newest first). Preserves recent tool outputs in full. Truncates older large outputs and saves them to disk. Truncated outputs show last 30 lines + placeholder.

**Phase 2 — Split and Compress**: Splits history at `1 - COMPRESSION_PRESERVE_THRESHOLD` (keeps 30% recent). Sends older portion to a summarizer model. Uses original history if the result is within token limits.

**Phase 3 — Self-Correcting Verification**: A secondary model call validates the compression for completeness. Ensures no critical file paths or error messages are lost. Updates `<state_snapshot>` with corrections.

**Phase 4 — Validation**: Recomputes token count after compression. Rejects the compression if tokens increased. Falls back gracefully on summarization failure.

### 2.3 Session Management

File: `packages/core/src/utils/sessionUtils.ts`, `sessionOperations.ts`

Sessions convert to Gemini client history format:
- Filters info/error/warning messages
- Preserves tool calls and function responses
- Maintains multimodal part structure (text, functionCall, functionResponse)
- Handles thoughts as text parts with `thought: true` marker

---

## 3. Prompt & Context Engineering

### 3.1 Hierarchical Memory System

File: `packages/core/src/utils/memoryDiscovery.ts` (915 lines)

The system uses a **SCATTER-GATHER-CATEGORIZE-DEDUPLICATE** pattern:

```
SCATTER: Discover all GEMINI.md files in parallel
  ├─ Global paths (from ~/.gemini/)
  ├─ Extension paths (from VS Code extensions)
  ├─ Project paths (upward traversal from CWD to .git root)
  └─ User project memory (from project-specific storage)

GATHER: Read all files concurrently (batches of 10 to prevent EMFILE errors)

CATEGORIZE: Organize into HierarchicalMemory structure
  {
    global: string;
    extension: string;
    project: string;
    userProjectMemory: string;
  }

DEDUPLICATE: By file identity (device + inode) to handle case-insensitive filesystems
```

Memory injection into the system prompt:
```markdown
# User Memory

--- Global ---
[global memory content]

--- User Project Memory ---
[user-provided project memory]

--- Extension ---
[extension context]

--- Project ---
[project memory + MCP instructions]
```

### 3.2 JIT (Just-In-Time) Context Loading

File: `packages/core/src/tools/jit-context.ts`

When tools access files, the system discovers contextual GEMINI.md files from that access path upward to the project root:

**Triggering tools**: `read_file`, `list_directory`, `write_file`, `replace`, `read_many_files`

**Mechanism**:
- Traverses from accessed file's parent directory upward to git root
- Respects trusted roots boundary
- Caches file identities to prevent duplicate loading
- Returns empty string if JIT disabled (safe fallback)

**Appended format**:
```
--- Newly Discovered Project Context ---
[content from GEMINI.md files]
--- End Project Context ---
```

This is one of Gemini CLI's most distinctive features: instruction files are not just discovered at startup, but dynamically expanded as the agent explores the codebase.

### 3.3 Import Processing

File: `packages/core/src/utils/memoryImportProcessor.ts`

GEMINI.md files support `#import` directives:
- Prevents circular imports via `ImportState` tracking
- Maintains import tree structure for debugging
- Respects boundary markers (.git) for safety
- Has depth limit protection

### 3.4 Prompt Composition

File: `packages/core/src/prompts/promptProvider.ts` (322 lines)

The PromptProvider uses a **modular snippets-based approach**:

```typescript
interface SystemPromptOptions {
  preamble?: { interactive: boolean }
  coreMandates?: { interactive, hasSkills, hasHierarchicalMemory, contextFilenames }
  subAgents?: AgentDefinition[]
  agentSkills?: SkillInfo[]
  taskTracker?: string
  hookContext?: boolean
  primaryWorkflows?: { ... }
  planningWorkflow?: { ... }
  operationalGuidelines?: { ... }
  sandbox?: { mode, toolSandboxingEnabled }
  interactiveYoloMode?: boolean
  gitRepo?: boolean
  finalReminder?: { readFileToolName }
}
```

Two paths for construction:
- **Custom override**: If `GEMINI_SYSTEM_MD` env var set, use custom template
- **Standard composition**: Build from modular snippets

### 3.5 Active Topic Narration

At the end of prompt construction, if topic update narration is enabled:
```
[Active Topic: {sanitized topic string}]
```

This helps the model stay focused on the current work thread, especially after compression or tool-heavy turns.

---

## 4. Harness Engineering: Context Compression

### 4.1 Tool Output Distillation

File: `packages/core/src/context/toolDistillationService.ts`

Manages oversized tool outputs through progressive distillation:

1. **Thresholding**: `maxTokens * 4` chars trigger distillation
2. **Structural Truncation**: Head/tail preservation (default 20%/80% split)
3. **Intent Summarization**: Secondary model call extracts critical data:
   - Exact error messages
   - File paths and line numbers
   - Definitive outcomes
4. **Disk Offloading**: Full output saved to temp directory
5. **Exempt Tools**: `read_file` and `read_many_files` bypass distillation

### 4.2 Per-File Content Routing

File: `packages/core/src/context/contextCompressionService.ts`

Novel **per-file content routing system** — instead of binary include/exclude, each file is routed to one of four tiers:

| Route | Behavior | When Used |
|-------|----------|-----------|
| `FULL` | Content fully retained | Small files or recent access |
| `PARTIAL` | Show specific line ranges `[start-end]` | Medium files with known regions of interest |
| `SUMMARY` | 2-3 line technical summary (cached) | Large files not recently accessed |
| `EXCLUDED` | File reference omitted entirely | Irrelevant files |

**Protection Window**: Recent 2 turns fully protected. Tracks which files accessed recently via `read_file` tool calls. Cache per file with content hash tracking.

**Batched Routing**: A single model call routes all pending files simultaneously, reducing API overhead.

### 4.3 Tool Output Masking

File: `packages/core/src/context/toolOutputMaskingService.ts`

**Hybrid Backward-Scanned FIFO Algorithm**:

1. **Protection Window**: Newest 50K tokens protected from pruning
2. **Scan Backward**: Find all remaining unmasked tool outputs
3. **Batch Trigger**: Only mask if `totalPrunableTokens > 30K`
4. **Replacement**: Masked outputs become:
   ```
   [tool_output_masked: {toolName}, {lines} → {outputFile}]
   ```

**Exempt tools** (never masked): `activate_skill`, `save_memory`, `ask_user`, `enter_plan_mode`, `exit_plan_mode`

### 4.4 Context Truncation Strategies

File: `packages/core/src/context/truncation.ts`

**Proportional head/tail preservation**:
```
truncateProportionally(str, targetChars, prefix, headRatio=0.2):
  head = availableChars * 0.2
  tail = availableChars * 0.8
  result = "{prefix}\n{head}\n...\n{tail}"
```

Token estimation:
- `ASCII_TOKENS_PER_CHAR` ≈ 0.25
- `NON_ASCII_TOKENS_PER_CHAR` ≈ 1.0

Safe JSON-aware truncation for function responses: preserves schema keys (stdout, stderr), truncates individual large string values.

---

## 5. Persistent Memory System

### 5.1 Memory Tool (Save Memory)

File: `packages/core/src/tools/memoryTool.ts`

Implements persistent long-term memory with dual scope:

**Global Memory** (`~/.gemini/GEMINI.md`):
- Cross-project user preferences
- Personal habits and conventions
- Loaded once per session refresh

**Project Memory** (`./.gemini/GEMINI.md`):
- Project-specific context (architecture, team info)
- Accessible only in trusted folders

**Memory Section Format**:
```markdown
## Gemini Added Memories
- Memory fact (single-line, sanitized)
- Another fact
```

Safety: Newline sanitization prevents markdown injection. Allowlist tracking for already-confirmed edits. Diff-based confirmation UI.

### 5.2 Memory Manager Agent

File: `packages/core/src/agents/memory-manager-agent.ts`

A dedicated sub-agent for semantic memory maintenance:

**Operations**:
1. **Adding**: Route to appropriate store (global/project/subdirectory)
2. **De-duplicating**: Combine semantically equivalent entries
3. **Organizing**: Restructure for clarity
4. **Cleaning**: Remove stale entries

**Three-tier routing**:
```
Global (~/.gemini/GEMINI.md)
├─ User preferences, personal info, cross-project habits

Project (./GEMINI.md)
├─ Architecture decisions, team conventions, references to subdirectory memories

Subdirectories (src/GEMINI.md, etc.)
└─ Module-specific detailed context
```

Uses `ask_user` tool for ambiguity resolution when memory could belong to multiple tiers.

---

## 6. State Snapshot Pattern

### 6.1 Intent Summary Tags

Structured summary format used after compression:
```xml
<intent_summary>
- **Primary Goal**: User's ultimate objective
- **Verified Facts**: Definitively completed items
- **Working Set**: Current files under analysis
- **Active Blockers**: Exact error messages/failing tests
</intent_summary>
```

### 6.2 State Snapshots

Cumulative workspace state integrating information from previous snapshots:
```xml
<state_snapshot>
[Structured markdown describing current workspace state]
[Integrates information from previous snapshots]
[Updates with recent events]
</state_snapshot>
```

Updated (not appended) on each compression cycle. A secondary model call validates and corrects the snapshot if needed.

---

## 7. Comparison with ycode

| Feature | Gemini CLI | ycode |
|---------|-----------|-------|
| **Memory scoping** | 4 tiers (global/extension/project/subdirectory) | 2 tiers (global/project) |
| **Instruction loading** | JIT + startup discovery | JIT + startup discovery (adopted) |
| **Import directive** | `#import` with depth protection | `#import` with depth protection (adopted) |
| **Compression** | 4-phase with model-assisted verification | 3-layer defense (prune → compact → flush) |
| **File routing** | Per-file FULL/PARTIAL/SUMMARY/EXCLUDED | Per-file routing (adopted) |
| **Tool distillation** | Model-assisted summarization | Heuristic head/tail with disk save (adopted, simpler) |
| **State snapshots** | Model-verified `<state_snapshot>` | Cumulative `StateSnapshot` (adopted) |
| **Intent summary** | `<intent_summary>` with 4 categories | `<intent_summary>` with 5 categories (adopted, extended) |
| **Topic tracking** | `[Active Topic: ...]` injection | `[Active Topic: ...]` injection (adopted) |
| **Memory manager** | Dedicated sub-agent | Background Dreamer (simpler) |
| **Context window** | 1M+ tokens (Gemini) | ~200K tokens (Anthropic/OpenAI) |
| **Storage** | Markdown files | Markdown files with YAML frontmatter |

### Key Differences

1. **Model-Assisted vs Heuristic**: Gemini CLI uses secondary model calls for tool output distillation, compression verification, and file routing. ycode uses heuristic-only approaches to avoid the API cost and latency of additional model calls.

2. **Extension Tier**: Gemini CLI integrates with VS Code extensions as a memory source. ycode has no IDE-extension memory tier (it's a pure CLI tool).

3. **Context Window Strategy**: With Gemini's 1M+ context, Gemini CLI can afford less aggressive compression. ycode targets 200K-token models and must be more aggressive with pruning and compaction.

4. **Memory Management**: Gemini CLI uses a dedicated sub-agent for memory maintenance. ycode uses a background goroutine (Dreamer) that performs stale removal and duplicate merging without model calls.

---

## 8. Features Adopted by ycode

The following features from Gemini CLI were adopted in ycode's Phase 2 memory improvements:

| Feature | Adaptation |
|---------|------------|
| JIT subdirectory context loading | Implemented as `JITDiscovery` with tool access hooks |
| `#import` directive | `ResolveImports()` with circular detection, max depth 3 |
| Structured intent summary | Extended from 4 to 5 categories (added Decision Log) |
| State snapshot pattern | Cumulative `StateSnapshot` with completed steps tracking |
| Active topic tracking | `TopicTracker` with stale timeout (20 turns) |
| Hierarchical memory scopes | Global + project dual-store with scope-based routing |
| Per-file content routing | `RoutingCache` with FULL/PARTIAL/SUMMARY/EXCLUDED classification |
| Tool output distillation | Heuristic-only (no model call), head/tail with disk save |
| Startup prewarming | Concurrent goroutines for parallel initialization |

### Features Not Adopted

| Feature | Reason |
|---------|--------|
| Memory manager sub-agent | Background Dreamer already covers maintenance; sub-agent adds API cost |
| Model-assisted compression verification | Adds latency and cost; heuristic approach is sufficient |
| Extension memory tier | ycode is a pure CLI tool with no IDE integration |

---

## 9. Key Design Insights

### 9.1 JIT Context as Lazy Initialization

Gemini CLI's JIT discovery pattern treats instruction files as lazily-initialized context. The startup discovery provides a baseline, but the real context expands as the agent works. This matches how developers work: you don't know which subdirectories are relevant until you start reading code.

### 9.2 Batched Model Calls for Routing

Rather than making individual model calls per file, Gemini CLI batches all pending files into a single routing call. This amortizes the latency across many decisions — an important pattern for context compression at scale.

### 9.3 Self-Correcting Compression

The Phase 3 verification step in chat compression is unique: a secondary model validates the compression output, checking that critical information (error messages, file paths) survived. If not, it patches the `<state_snapshot>`. This is expensive but prevents silent information loss.

### 9.4 Protection Windows as Temporal Locality

The protection window concept (recent N tokens/turns fully protected) exploits temporal locality: the most recent context is overwhelmingly the most likely to be referenced. This matches CPU cache strategies — hot data stays uncompressed.

---

## 10. File Reference

| Concept | Gemini CLI Source File |
|---------|----------------------|
| Memory context manager | `packages/core/src/context/memoryContextManager.ts` |
| Memory discovery | `packages/core/src/utils/memoryDiscovery.ts` |
| Import processor | `packages/core/src/utils/memoryImportProcessor.ts` |
| Memory tool (save) | `packages/core/src/tools/memoryTool.ts` |
| Memory manager agent | `packages/core/src/agents/memory-manager-agent.ts` |
| Chat compression | `packages/core/src/context/chatCompressionService.ts` |
| Agent history | `packages/core/src/context/agentHistoryProvider.ts` |
| Context compression (file routing) | `packages/core/src/context/contextCompressionService.ts` |
| Tool output distillation | `packages/core/src/context/toolDistillationService.ts` |
| Tool output masking | `packages/core/src/context/toolOutputMaskingService.ts` |
| Truncation utilities | `packages/core/src/context/truncation.ts` |
| JIT context | `packages/core/src/tools/jit-context.ts` |
| Prompt provider | `packages/core/src/prompts/promptProvider.ts` |
| Session utilities | `packages/core/src/utils/sessionUtils.ts` |

---

*This analysis is based on the Gemini CLI codebase (`x/gemini-cli/`) as of April 2026.*

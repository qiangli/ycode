# Memory Management in Cline (x/cline/)

This document summarizes how Cline (the TypeScript VS Code extension agent) handles multi-turn conversations, context engineering, harness engineering, and memory management.

---

## 1. Architecture Overview

Cline is a task-oriented VS Code extension agent. Each interaction is a **task** with a unique ID, persistent state, and isolated conversation history. The codebase is TypeScript with a component-based prompt system and model-family variant selection.

---

## 2. Task-Based Memory Model

### Task Lifecycle

Each task has a unique `taskId` (UUID) and ULID. Task state is split across three layers:

| Layer | Format | Storage | Purpose |
|-------|--------|---------|---------|
| Cline Messages | JSON | `globalStorage/tasks/{taskId}/cline_messages.json` | UI-visible chat messages |
| API Conversation History | JSON | `globalStorage/tasks/{taskId}/api_conversation_history.json` | Anthropic `MessageParam[]` format |
| Task Metadata | JSON | `globalStorage/tasks/{taskId}/task_metadata.json` | Files in context, metrics, state |

Key files:
- `src/core/task/index.ts` — Task class, agentic loop, tool execution
- `src/core/task/message-state.ts` — Message state with mutex-protected mutations
- `src/core/storage/disk.ts` — Disk persistence for all task data

### Task Resumption

Tasks can be resumed from disk:
1. Loads saved `clineMessages` from JSON
2. Restores `apiConversationHistory`
3. Initializes context history for previous edits
4. Shows resume button (`resume_task` or `resume_completed_task`)

### Message Index Tracking

Each message stores its `conversationHistoryIndex` and `conversationHistoryDeletedRange`, enabling reconstruction of conversation state at any point, including after context truncation.

---

## 3. Multi-Turn Conversation Loop

### Agentic Loop (`initiateTaskLoop`)

```
while (!abort) {
    didEnd = recursivelyMakeClineRequests(userContent, includeFileDetails)
    if (didEnd) break
    // No tools used → ask about completion
    consecutiveMistakeCount++
}
```

### Turn Cycle

1. **Construct user message**: load context (file mentions, slash commands), add environment details, optionally add auto-summarization prompt
2. **Send to API**: system prompt with variant selection, streaming response
3. **Accumulate tool calls**: `processToolUseDelta()` for streaming tool blocks
4. **Execute tools**: collect `ToolResultBlockParam` results
5. **Recurse**: feed tool results as next user message

### Stop Conditions

- Abort flag
- `attempt_completion` tool invocation
- Max consecutive mistakes reached (`maxConsecutiveMistakes` setting)
- YOLO mode: auto-fail task on max mistakes

### Error Recovery

- **Context overflow**: automatic retry up to 3 times with exponential backoff (2s, 4s, 8s)
- **Context truncation**: triggered via `getTruncatedMessages()` using `conversationHistoryDeletedRange`
- **Streaming failure**: `abortStream()` saves partial state with `[Task cancelled/interrupted]` marker

---

## 4. System Prompt Engineering

### Model-Family Variant System

Cline selects system prompt variants per model family:

| Variant | Model Family |
|---------|-------------|
| `native-gpt-5` / `native-gpt-5-1` | GPT-5 |
| `gemini-3` | Google Gemini 3 |
| `devstral` | Mistral |
| `hermes` | Hermes |
| `glm` | GLM |
| `generic` | Fallback for all others |

File: `src/core/prompts/system-prompt/registry/PromptRegistry.ts`

### Component-Based Assembly

The `PromptBuilder` assembles prompts from composable components:

| Component | Purpose |
|-----------|---------|
| `agent_role` | Cline's responsibilities |
| `objective` | Task goal statement |
| `capabilities` | Tool availability |
| `act_vs_plan_mode` | Mode-specific instructions |
| `rules` | User-defined `.clinerules` |
| `skills` | User-created skill files |
| `tool_use` | Tool specifications with formatting |
| `mcp` | MCP server documentation |
| `task_progress` | Focus chain checklist |
| `feedback` | Response format templates |
| `system_info` | System capabilities/environment |
| `user_instructions` | Global/local rules + workflows |
| `editing_files` | File modification conventions |

File: `src/core/prompts/system-prompt/components/index.ts`

### Dynamic Context Injection

Each request injects dynamic context including:
- `mcpHub` — live MCP server availability
- `skills` — discovered available skills
- `focusChainSettings` — todo list mode
- `globalClineRulesFileInstructions` / `localClineRulesFileInstructions`
- `clineIgnoreInstructions` — ignore patterns
- `preferredLanguageInstructions`
- `yoloModeToggled` — auto-approve mode
- `enableNativeToolCalls` / `enableParallelToolCalling`

---

## 5. Context Window Management

### Token Budget

Context window reserves vary by model size:

| Context Window | Reserved | Effective Max |
|---------------|----------|---------------|
| 64K | 27K | 37K |
| 128K | 30K | 98K |
| 200K | 40K | 160K |
| Other | max(40K, 20%) | 80% |

File: `src/core/context/context-management/context-window-utils.ts`

### Three Compaction Strategies

**1. Context Truncation** (primary):
- Trigger: previous request's total tokens >= `maxAllowedSize`
- Binary search on `conversationHistoryDeletedRange`
- Deleted messages are completely dropped from API requests
- Range stored as `[start, end]` tuple

**2. Auto-Condense** (for next-gen models):
- Enabled via `useAutoCondense` setting + `isNextGenModelFamily()`
- Triggers `summarize_task` tool to create summary
- Increments `conversationHistoryDeletedRange` by 2 per condense

**3. File Read Optimization**:
- `attemptFileReadOptimization()` rewrites large file content blocks
- Replaces with inline diffs/summaries
- Avoids truncation if optimization saves enough tokens

---

## 6. File Context Management

### File Tracking

File: `src/core/context/context-tracking/FileContextTracker.ts`

Each file in context is tracked with metadata:
- `path` — absolute file path
- `record_state` — `"active"` or `"stale"` (old reads marked stale)
- `record_source` — `"read_tool"` | `"cline_edited"` | `"user_edited"` | `"file_mentioned"`
- `cline_read_date`, `cline_edit_date`, `user_edit_date` — timestamps

### File Watchers

One `chokidar` watcher per tracked file:
- 100ms stabilization threshold before emitting change
- Distinguishes user edits from Cline's own edits via `recentlyEditedByCline` set
- Atomic write handling enabled

### Dedup Cache

File reads are deduplicated by `{path → {readCount, mtime, imageBlock?}}` to avoid re-reading unchanged files.

---

## 7. Permission System

### Hierarchical Approval

```
YOLO Mode → All Auto-Approve → Granular Settings → Ask User
```

Granular settings:
- `readFiles` / `readFilesExternally`
- `editFiles` / `editFilesExternally`
- `executeSafeCommands` / `executeAllCommands`
- `useBrowser` / `useMcp`

### Path-Based Distinction

`shouldAutoApproveToolWithPath()` distinguishes local (workspace) vs external file operations, with separate approval flags for each. Multi-root workspace aware.

### Command Validation

`CommandPermissionController` validates shell commands against allow/deny regex patterns, including redirect and subshell checking.

---

## 8. Checkpointing & Undo

### Shadow Git Repository

File: `src/integrations/checkpoints/CheckpointTracker.ts`

Cline creates an **isolated shadow Git repository** per workspace (separate from user's repo):
- Commit on first API request per task
- Message format: `"checkpoint-{cwdHash}-{taskId}"`
- All tracked workspace files included
- Exclusions via `.gitexclude`

### Restore Capability

- **Checkpoint restore**: resets working directory to checkpoint state
- **Diff viewing**: shows changes between current state and any checkpoint
- Lock acquisition for safe concurrent access

### Context History Tracking

`ContextManager` maintains `contextHistoryUpdates: Map<messageIndex, [EditType, Map<blockIndex, ContextUpdate[]>]>`:
- Tracks all content modifications by timestamp
- Serialized to disk
- Enables rewinding context changes when restoring checkpoints

Edit types: `NO_FILE_READ`, `READ_FILE_TOOL`, `ALTER_FILE_TOOL`, `FILE_MENTION`

---

## 9. Loop Detection

File: `src/core/task/loop-detection.ts`

| Threshold | Value | Action |
|-----------|-------|--------|
| Soft | 3 consecutive similar responses | Warn user |
| Hard | 5 consecutive similar responses | Break loop |

---

## 10. Key Constants

| Constant | Value | File |
|----------|-------|------|
| Context Reserve (128K) | 30,000 tokens | `context-window-utils.ts:25` |
| Context Reserve (200K) | 40,000 tokens | `context-window-utils.ts:28` |
| Auto-Retry Max | 3 attempts | `index.ts:2104` |
| Retry Backoff Base | 2,000ms | `index.ts:2110` |
| Loop Soft Threshold | 3 | `loop-detection.ts:21` |
| Loop Hard Threshold | 5 | `loop-detection.ts:22` |
| File Stabilization | 100ms | `FileContextTracker.ts:62` |
| MCP Timeout | 10,000ms | `index.ts:1864` |

---

## 11. Comparison with ycode

| Feature | Cline | ycode |
|---------|-------|-------|
| **Memory model** | Task-based with per-task storage | Session-based with JSONL |
| **Compaction** | Range deletion + auto-condense + file optimization | 3-layer (prune → compact → flush) |
| **System prompt** | Model-family variants + component assembly | Section-based with static/dynamic boundary |
| **Checkpointing** | Shadow Git repository | Scratchpad checkpoints |
| **File tracking** | Per-file watchers with edit detection | No file-level tracking |
| **Loop detection** | Soft/hard thresholds (3/5) | No loop detection |
| **Permission** | Hierarchical (YOLO → All → Granular → Ask) | 3-mode (ReadOnly → Workspace → Full) |
| **Context budget** | Model-specific reserves (27-40K) | Fixed 100K threshold |
| **Persistent memory** | Task metadata only | Typed markdown with staleness |

### Key Features ycode Could Adopt

1. **Loop detection** — detect repeated similar responses and break infinite loops
2. **File context tracking** — per-file watchers with edit source attribution
3. **Model-specific context budgets** — adjust thresholds per model's context window
4. **Auto-condense via tool** — use LLM to summarize (vs. ycode's heuristic summary)
5. **Shadow Git checkpoints** — isolated from user's repo, enables true undo
6. **Context history tracking** — track all modifications by timestamp for rewind

---

*This analysis is based on the Cline codebase as of April 2025.*

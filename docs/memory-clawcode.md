# Memory Management in Claw Code (priorart/clawcode/)

This document summarizes how Claw Code (the Rust CLI agent harness in `priorart/clawcode/`) handles multi-turn conversation, context engineering, harness engineering, and memory management. Claw Code is the reference implementation that ycode was based on.

---

## Architecture Overview

Claw Code is structured as a Rust workspace with 9 crates:

```
rusty-claude-cli    ← main binary: REPL, CLI arg parsing, rendering
  ├── commands      ← slash-command registry & dispatch
  ├── runtime       ← core engine: conversation loop, session, config, permissions,
  │                   MCP, hooks, file ops, bash execution, git context
  ├── api           ← provider clients (Anthropic + OpenAI-compat), streaming, auth
  ├── tools         ← tool definitions & execution (Bash, Read/Write/Edit, Glob, Grep, etc.)
  ├── plugins       ← plugin manager, metadata, install/enable/disable
  └── telemetry     ← session tracing & usage analytics

mock-anthropic-service  ← deterministic mock server for integration tests
compat-harness          ← TS manifest extraction utility
```

---

## Multi-Turn Conversation Handling

### Core Conversation Loop (`runtime/src/conversation.rs`)

The `ConversationRuntime` struct coordinates the model loop, tool execution, hooks, and session updates:

```rust
pub struct ConversationRuntime<C, T> {
    session: Session,
    api_client: C,
    tool_executor: T,
    permission_policy: PermissionPolicy,
    system_prompt: Vec<String>,
    max_iterations: usize,
    usage_tracker: UsageTracker,
    hook_runner: HookRunner,
    auto_compaction_input_tokens_threshold: u32,
    // ...
}
```

**Turn Execution Flow:**

1. **User Input Processing**: `run_turn()` accepts user input and pushes it to the session
2. **API Request Building**: Constructs `ApiRequest` with system prompt + conversation messages
3. **Streaming Response**: Calls `api_client.stream()` to get assistant events
4. **Message Assembly**: Builds assistant message from streamed events (text deltas, tool uses)
5. **Tool Execution**: For each `ToolUse` block:
   - Runs pre-tool hooks
   - Checks permissions via `PermissionPolicy`
   - Executes tool via `ToolExecutor`
   - Runs post-tool hooks
   - Pushes tool result back to session
6. **Iteration Control**: Loop continues until no pending tool uses or max iterations reached
7. **Auto-Compaction**: Optionally compacts session if token threshold exceeded

### Message Types (`runtime/src/session.rs`)

```rust
pub enum MessageRole {
    System,
    User,
    Assistant,
    Tool,
}

pub enum ContentBlock {
    Text { text: String },
    ToolUse { id: String, name: String, input: String },
    ToolResult { tool_use_id: String, tool_name: String, output: String, is_error: bool },
}
```

---

## Context Engineering

### System Prompt Structure (`runtime/src/prompt.rs`)

The prompt builder assembles sections in a specific order with a **dynamic boundary marker**:

```rust
pub const SYSTEM_PROMPT_DYNAMIC_BOUNDARY: &str = "__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__";
```

**Static Sections (cacheable):**
1. Simple intro section (role description)
2. Output style section (optional)
3. Simple system section (rules)
4. Simple doing tasks section
5. Actions section

**Dynamic Sections (below boundary):**
1. Environment context (model, working directory, date, platform)
2. Project context (git status, recent commits, staged files)
3. Instruction files (CLAUDE.md ancestry)
4. Runtime config

### Instruction File Discovery

Claw Code walks the directory ancestry from CWD to root searching for:

- `CLAUDE.md`
- `CLAUDE.local.md`
- `.claw/CLAUDE.md`
- `.claw/instructions.md`

**Budget Constraints:**
- Max 4,000 chars per instruction file
- Max 12,000 chars total across all files
- Files deduplicated by content hash

```rust
const MAX_INSTRUCTION_FILE_CHARS: usize = 4_000;
const MAX_TOTAL_INSTRUCTION_CHARS: usize = 12_000;
```

### Git Context Injection (`runtime/src/git_context.rs`)

The `GitContext` struct gathers git-aware context at startup:

```rust
pub struct GitContext {
    pub branch: Option<String>,
    pub recent_commits: Vec<GitCommitEntry>,  // Last 5 commits
    pub staged_files: Vec<String>,
}
```

Git context includes:
- Current branch name
- Recent commits (hash + subject)
- Staged files list
- Git status snapshot
- Git diff (staged and unstaged changes)

---

## Session Persistence & Memory Management

### Session Storage (`runtime/src/session.rs`)

Sessions are persisted as **JSONL** (newline-delimited JSON) files:

```rust
const ROTATE_AFTER_BYTES: u64 = 256 * 1024;  // 256 KB rotation threshold
const MAX_ROTATED_FILES: usize = 3;
```

**Storage Location:** `.claw/sessions/{session_id}.jsonl`

**JSONL Record Types:**
- `session_meta` — Session metadata (version, ID, timestamps, fork info)
- `message` — Conversation messages
- `compaction` — Compaction summaries
- `prompt_history` — User prompt entries with timestamps

**Rotation Strategy:**
- Files rotate at 256KB (`ROTATE_AFTER_BYTES`)
- Keeps 3 backups (`.1`, `.2`, `.3`)
- Prevents unbounded log growth

### Session Structure

```rust
pub struct Session {
    pub version: u32,
    pub session_id: String,
    pub created_at_ms: u64,
    pub updated_at_ms: u64,
    pub messages: Vec<ConversationMessage>,
    pub compaction: Option<SessionCompaction>,
    pub fork: Option<SessionFork>,
    pub workspace_root: Option<PathBuf>,
    pub prompt_history: Vec<SessionPromptEntry>,
    pub model: Option<String>,
    // ...
}
```

**Key Features:**
- Workspace binding to prevent cross-workspace contamination
- Fork tracking for session branching
- Compaction history
- Model persistence for resumed sessions

---

## Compaction & Context Compression

### Auto-Compaction (`runtime/src/compact.rs`)

Automatic compaction triggers when input tokens exceed a threshold (default: 100,000):

```rust
const DEFAULT_AUTO_COMPACTION_INPUT_TOKENS_THRESHOLD: u32 = 100_000;
```

**Compaction Strategy:**

1. **Preserve Recent Messages**: Keeps last N messages (default: 4)
2. **Tool-Use/Tool-Result Pair Integrity**: Never splits tool pairs
3. **Summarize Older Messages**: Generates structured summary

**Summary Structure:**

```rust
pub struct CompactionResult {
    pub summary: String,
    pub formatted_summary: String,
    pub compacted_session: Session,
    pub removed_message_count: usize,
}
```

**Summary Content:**
- Scope: Message counts by role (user/assistant/tool)
- Tools mentioned: Deduplicated tool names
- Recent user requests: Last 3 requests (160 chars each)
- Pending work: Lines containing "todo", "next", "pending", "follow up"
- Key files referenced: Up to 8 file paths from content
- Current work: 200-char excerpt from most recent message
- Key timeline: Role-labeled one-line recaps

### Summary Compression (`runtime/src/summary_compression.rs`)

Structured summaries are compressed with priority-based line selection:

```rust
const DEFAULT_MAX_CHARS: usize = 1_200;
const DEFAULT_MAX_LINES: usize = 24;
const DEFAULT_MAX_LINE_CHARS: usize = 160;
```

**Priority Tiers:**
- P0 (core): Summary header, Scope, Current work, Pending work, Key files, Tools
- P1 (headers): Section headers (lines ending with `:`)
- P2 (details): Bullet points (`- ` or `  - `)
- P3 (other): Everything else

**Processing:**
- Whitespace normalization (collapse consecutive blanks)
- Deduplication (case-insensitive)
- Truncation with omission notice

---

## Prompt Cache Optimization

### Cache Fingerprinting (`api/src/prompt_cache.rs`)

Claw Code implements prompt cache fingerprinting for cost optimization:

```rust
const DEFAULT_COMPLETION_TTL_SECS: u64 = 30;
const DEFAULT_PROMPT_TTL_SECS: u64 = 5 * 60;  // 5 minutes
```

**Request Fingerprints:**
- Model hash
- System prompt hash
- Tools hash
- Messages hash

**Cache Behavior:**
- Cache read tokens cost ~$0.30/1M vs $3.00/1M regular input (90% discount)
- Cache invalidated when fingerprints change
- Tracks unexpected cache breaks (fingerprint stable but tokens dropped)

### Dynamic Boundary Strategy

The `__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__` marker separates:

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
└───────────────────────────────────┘
```

This design allows dynamic context (memories, git status, instruction files) to change without invalidating the expensive static prompt cache.

---

## Usage Tracking & Cost Management

### Token Usage (`runtime/src/usage.rs`)

```rust
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct TokenUsage {
    pub input_tokens: u32,
    pub output_tokens: u32,
    pub cache_creation_input_tokens: u32,
    pub cache_read_input_tokens: u32,
}
```

**Usage Tracking:**
- Per-turn usage recording
- Cumulative session usage
- Cost estimation with model-specific pricing
- UsageTracker reconstructs from session messages

**Pricing Tiers:**
- Sonnet: $15/M input, $75/M output, $1.5/M cache read
- Haiku: $1/M input, $5/M output, $0.1/M cache read
- Opus: $15/M input, $75/M output, $1.5/M cache read

---

## Permission & Safety Engineering

### Permission Enforcement (`runtime/src/permission_enforcer.rs`)

Three permission modes:
- `read-only`: File reads, git operations
- `workspace-write`: File writes, edits
- `danger-full-access`: Bash execution, destructive operations

Permission checking happens:
1. Pre-tool hook (can cancel/modify)
2. Policy enforcement (can deny)
3. User prompt (if policy requires)

---

## Key Differences from ycode

| Aspect | Claw Code | ycode |
|--------|-----------|-------|
| Language | Rust | Go |
| Instruction files | `.claw/CLAUDE.md` | `.ycode/CLAUDE.md` |
| Session storage | `.claw/sessions/` | `~/.local/share/ycode/sessions/` |
| Config files | `.claw.json`, `~/.claw.json` | `.ycode/settings.json`, `~/.ycode/settings.json` |
| Persistent memory | External (OmX layer) | Built-in file-based store (`~/.ycode/projects/`) |
| Memory types | No built-in types | `user`, `feedback`, `project`, `reference` |
| Staleness/aging | Not built-in | Automatic with type-specific thresholds |
| Background consolidation | Not built-in | `Dreamer` goroutine |
| Relevance scoring | Not built-in | Weighted field matching + temporal decay |

---

## Design Philosophy

Claw Code follows a **modular, layered approach**:

1. **Core runtime** handles conversation, sessions, and tool execution
2. **External systems** (OmX, clawhip, OmO) handle higher-level concerns:
   - Multi-agent coordination
   - Notification routing (outside context window)
   - Planning and verification loops

This reflects the philosophy that **notification routing and monitoring should stay outside the coding agent's context window** so agents can focus on implementation.

---

## File Reference

| Concept | Source File |
|--------|-------------|
| Conversation runtime | `rust/crates/runtime/src/conversation.rs` |
| Session persistence | `rust/crates/runtime/src/session.rs` |
| Prompt builder | `rust/crates/runtime/src/prompt.rs` |
| Auto-compaction | `rust/crates/runtime/src/compact.rs` |
| Summary compression | `rust/crates/runtime/src/summary_compression.rs` |
| Git context | `rust/crates/runtime/src/git_context.rs` |
| Prompt cache | `rust/crates/api/src/prompt_cache.rs` |
| Usage tracking | `rust/crates/runtime/src/usage.rs` |
| Permission enforcement | `rust/crates/runtime/src/permission_enforcer.rs` |
| Config loading | `rust/crates/runtime/src/config.rs` |

# Codex Memory Management: Multi-Turn, Prompt Engineering, and Harness Design

This document analyzes how OpenAI's Codex CLI handles multi-turn conversations, prompt/context engineering, and harness engineering with respect to memory management. Codex is a Rust-based CLI agent that introduces several novel optimization techniques, most notably differential context injection.

---

## 1. Architecture Overview

Codex employs a **multi-layered context management system** organized around three core concepts:

| Concept | Purpose | Source |
|---------|---------|--------|
| **ContextManager** | Thread-local conversation history management | `core/src/context_manager/history.rs` |
| **SessionState** | Session-wide mutable state container | `core/src/state/session.rs` |
| **TurnContext** | Per-turn execution context and configuration | `core/src/codex.rs` |

### Core Philosophy

| Principle | Implementation |
|----------|----------------|
| **Differential updates** | Only emit changed context sections between turns |
| **Call-output invariants** | Every tool call must have a corresponding result |
| **Ghost state** | Internal snapshots preserved across compaction, hidden from model |
| **Atomic compaction** | Compaction as a full turn with proper lifecycle events |

---

## 2. Multi-Turn Conversation Handling

### 2.1 ContextManager (History)

File: `core/src/context_manager/history.rs`

```rust
ContextManager {
    items: Vec<ResponseItem>,              // Ordered oldest→newest
    history_version: u64,                   // Bumped on rewrite (compaction/rollback)
    token_info: Option<TokenUsageInfo>,    // API response token tracking
    reference_context_item: Option<TurnContextItem>,  // Baseline for context diffs
}
```

**Key feature**: `reference_context_item` enables differential context updates. When diffing turns, Codex compares against this snapshot instead of re-injecting everything.

### 2.2 ResponseItem Types

The system tracks diverse item types in conversation history:

| Type | Purpose |
|------|---------|
| `Message { role, content }` | User/assistant/developer messages |
| `FunctionCall` / `FunctionCallOutput` | Tool invocations and results |
| `CustomToolCall` / `CustomToolCallOutput` | Custom tool handling |
| `LocalShellCall` | Local shell execution |
| `Reasoning { encrypted_content }` | Extended thinking/reasoning blocks |
| `Compaction { encrypted_content }` | Compressed history summaries |
| `GhostSnapshot` | Internal state snapshots (not sent to model) |
| `ToolSearchCall` / `ToolSearchOutput` | Tool discovery operations |

### 2.3 Token Management

**Three-tier estimation strategy**:

1. **API-reported tokens**: Stored in `TokenUsageInfo.last_token_usage`
2. **Local estimation**: Byte-based heuristics for items added since last API call (`bytes / 4` with ceiling division)
3. **Image optimization**: Fixed estimate (~7,373 bytes per image) for inline base64; dimension-based calculation for `detail: "original"` images with LRU cache

### 2.4 Multi-Turn Rollback

File: `core/src/context_manager/history.rs:240-263`

```rust
drop_last_n_user_turns(num_turns: u32) {
    // Find user message positions (instruction turn boundaries)
    user_positions = [indices where role == "user"]
    
    // Calculate cut point
    cut_idx = if num_turns >= len then first_instruction_turn_idx
              else user_positions[len - num_turns]
    
    // Trim pre-turn contextual updates
    while cut_idx > first_instruction_turn_idx {
        if item is contextual developer/user message:
            cut_idx -= 1
            if mixed build_initial_context bundle detected:
                clear reference_context_item  // Force full reinjection next turn
        else:
            break
    }
    
    items.truncate(cut_idx)
}
```

**Special handling**: When rollback trims a mixed contextual+persistent developer bundle, the system clears `reference_context_item`. This forces the next turn to emit a full context reinjection instead of diffing against a stale baseline.

---

## 3. Prompt & Context Engineering

### 3.1 Differential Context Injection

File: `core/src/codex.rs:3704-3850`, `core/src/context_manager/updates.rs`

This is Codex's most distinctive feature. Instead of full prompt reinjection on every turn, Codex maintains a baseline snapshot and only emits **changes** as developer messages.

**Initial turn**: Full context bundle emitted:
- Model instructions
- Environment context (cwd, PATH, env vars)
- Permissions (sandbox/approval policy)
- Developer instructions (custom system prompts)
- Collaboration mode settings
- Personality selection
- Apps/MCP connector definitions
- Plugin summaries
- User instructions (AGENTS.md)

**Subsequent turns**: Only changed sections emitted:
```
Diff against reference_context_item:
  ├─ Model switch instructions (if model changed)
  ├─ Updated permissions (if sandbox/approval changed)
  ├─ Environment updates (if cwd/env changed)
  ├─ Collaboration mode changes
  ├─ Personality changes
  ├─ Apps/MCP connector updates
  ├─ Plugin summaries
  └─ User/AGENTS.md instructions
```

**Benefits**: Dramatically reduces input tokens in long conversations where the system prompt (~10-20K tokens) rarely changes between turns. The savings compound over many turns.

### 3.2 Previous Turn Settings Tracking

File: `core/src/codex.rs:2441-2450`

```rust
PreviousTurnSettings {
    model: String,           // Previous turn's model slug
    realtime_active: bool,   // Was realtime enabled?
}
```

On each user turn:
1. Diff `previous_turn_settings` against current `TurnContext`
2. Emit context updates only for changed fields
3. Update `previous_turn_settings` with current values

### 3.3 Contextual Fragment Markers

File: `core/src/instructions/fragment.rs`

```rust
ContextualUserFragmentDefinition {
    start_marker: &'static str,   // e.g., "# AGENTS.md instructions for "
    end_marker: &'static str,     // e.g., "</INSTRUCTIONS>"
}
```

Instructions wrapped with special markers are **auto-detected as contextual**. During rollback, these items are trimmed along with rolled-back turns. When a mixed bundle (persistent + contextual developer content) is trimmed, the system clears `reference_context_item` to force full reinjection.

### 3.4 TurnContext Structure

File: `core/src/codex.rs:865-910`

The `TurnContext` is a comprehensive per-turn configuration object:

```rust
TurnContext {
    // Identity
    sub_id: String,
    trace_id: Option<String>,
    config: Arc<Config>,
    model_info: ModelInfo,
    
    // Environment
    cwd: AbsolutePathBuf,
    environment: Option<Arc<Environment>>,
    current_date: Option<String>,
    timezone: Option<String>,
    
    // Instructions
    developer_instructions: Option<String>,
    user_instructions: Option<String>,
    compact_prompt: Option<String>,
    
    // Policy
    approval_policy: Constrained<AskForApproval>,
    sandbox_policy: Constrained<SandboxPolicy>,
    file_system_sandbox_policy: FileSystemSandboxPolicy,
    network_sandbox_policy: NetworkSandboxPolicy,
    
    // Model parameters
    reasoning_effort: Option<ReasoningEffortConfig>,
    reasoning_summary: ReasoningSummaryConfig,
    personality: Option<Personality>,
    
    // Features
    features: ManagedFeatures,
    tools_config: ToolsConfig,
    dynamic_tools: Vec<DynamicToolSpec>,
    
    // Performance
    truncation_policy: TruncationPolicy,
    ghost_snapshot: GhostSnapshotConfig,
    
    // Session
    collaboration_mode: CollaborationMode,
    turn_skills: TurnSkillsContext,
}
```

### 3.5 User Instructions (AGENTS.md)

File: `core/src/instructions/`

- Loaded from `.codex/AGENTS.md` or working directory
- Wrapped with contextual markers for automatic detection
- Preserved across turns if unchanged
- Can be modified or removed between turns

---

## 4. Harness Engineering: Compaction and Recovery

### 4.1 Three Compaction Strategies

File: `core/src/compact.rs`

| Strategy | Trigger | Context Handling |
|----------|---------|------------------|
| **Manual** | User `/compact` command | Clears `reference_context_item`; next turn fully reinjects context |
| **Pre-sampling** | Before model switch when context exceeds window | Strips trailing model-switch items; preserves baseline |
| **Mid-turn Auto** | During stream when approaching limits | Injects context above last user message |

### 4.2 Compaction Flow

```
run_compact_task()
  ├─ Create compaction turn with user input
  ├─ Iteratively trim oldest history items until prompt fits
  ├─ Send to model with SUMMARIZATION_PROMPT template
  ├─ Collect assistant response (summary text)
  ├─ Build replacement history:
  │  ├─ Include any pre-compaction initial context (conditional)
  │  └─ Append compaction summary as ResponseItem::Compaction
  ├─ Preserve GhostSnapshot items (internal state)
  └─ Replace history + set reference_context_item
```

The compaction summary is stored as `ResponseItem::Compaction { encrypted_content }` — the content may be encrypted/base64-encoded (for extended thinking blocks).

### 4.3 History Normalization

File: `core/src/context_manager/normalize.rs`

**Invariants enforced before every API request**:

1. **Call-Output Pairing**: Every function/tool call must have a corresponding output. Missing outputs are synthesized as `"aborted"`.

2. **Orphan Removal**: Every output must have a corresponding call. Orphaned outputs are removed with error logging.

3. **Image Support Detection**: Images stripped when model lacks `InputModality::Image`. Replaced with placeholder: `"image content omitted because you do not support image input"`.

4. **Corresponding Item Removal**: When items are rolled back, their pairs are removed:
   - Function calls ↔ Function outputs
   - Tool search calls ↔ Tool search outputs
   - Custom tool calls ↔ Custom tool outputs
   - Local shell calls ↔ Function outputs

### 4.4 Ghost Snapshots

Ghost snapshots are internal state records preserved across compaction but **never sent to the model**:

```rust
ResponseItem::GhostSnapshot {
    // Internal state data
}
```

- Filtered out in `for_prompt()` before API requests
- Survive compaction (explicitly preserved in `run_compact_task`)
- Enable state recovery after history rewriting

### 4.5 Encrypted Reasoning Items

Extended thinking/reasoning blocks are stored as opaque content:

```rust
ResponseItem::Reasoning {
    encrypted_content: String,  // Base64-encoded
}
```

Token estimation without decoding: `bytes * 3/4` (base64 decode ratio) minus 650-byte overhead. This reduces memory usage for thinking-heavy conversations.

---

## 5. Session State Management

### 5.1 Session Structure

File: `core/src/codex.rs:824-846`

```rust
Session {
    conversation_id: ThreadId,
    state: Mutex<SessionState>,
    conversation: Arc<RealtimeConversationManager>,
    active_turn: Mutex<Option<ActiveTurn>>,
    mailbox: Mailbox,                        // Inter-agent message queue
    idle_pending_input: Mutex<Vec<ResponseInputItem>>,
    services: SessionServices,
}

SessionState {
    session_configuration: SessionConfiguration,
    history: ContextManager,
    latest_rate_limits: Option<RateLimitSnapshot>,
    server_reasoning_included: bool,
    dependency_env: HashMap<String, String>,
    mcp_dependency_prompted: HashSet<String>,
    previous_turn_settings: Option<PreviousTurnSettings>,
    startup_prewarm: Option<SessionStartupPrewarmHandle>,
    active_connector_selection: HashSet<String>,
    granted_permissions: Option<PermissionProfile>,
}
```

### 5.2 Startup Prewarming

Optional pre-computed session state (`SessionStartupPrewarmHandle`) loaded during initialization. Reduces latency for session startup by front-loading expensive computations.

### 5.3 Memory Trace Integration

File: `core/src/memory_trace.rs`

Builds memory summaries from trace files via `/v1/memories/trace_summarize` API:
- Tracks raw memories and rollout summaries
- Detects added/removed memories across sessions
- Memory instructions injected into context when feature enabled

---

## 6. API Request Building

### 6.1 Prompt Assembly

File: `core/src/client_common.rs`

```rust
Prompt {
    input: Vec<ResponseItem>,        // Formatted conversation history
    tools: Vec<ToolSpec>,            // Available tools
    parallel_tool_calls: bool,
    base_instructions: BaseInstructions,  // System prompt
    personality: Option<Personality>,
    output_schema: Option<Value>,
}
```

**Assembly pipeline**:
1. Get history items from ContextManager
2. Apply `for_prompt(input_modalities)` — normalizes and filters
3. Build base_instructions from ModelInfo
4. Apply personality overrides if needed
5. Get formatted input via `get_formatted_input()`:
   - Special handling for Freeform apply_patch tool
   - Reserialize shell outputs as structured text
6. Create API request with payload

### 6.2 Compact Conversation History

File: `core/src/client.rs:413-478`

```rust
compact_conversation_history() {
    payload = ApiCompactionInput {
        model: model_info.slug,
        input: formatted_history,
        instructions: base_instructions.text,
        tools: tools_json,
        parallel_tool_calls: bool,
        reasoning: build_reasoning(model, effort, summary),
        text: create_text_param(verbosity, output_schema),
    }
    
    headers = {
        X_CODEX_INSTALLATION_ID: installation_id,
        conversation_headers: conversation_id,
    }
    
    ApiCompactClient::compact_input(payload, headers)
}
```

---

## 7. Comparison with ycode

| Feature | Codex | ycode |
|---------|-------|-------|
| **Language** | Rust | Go |
| **Differential context** | `reference_context_item` baseline diffing | `ContextBaseline` with section hashing (adopted) |
| **History normalization** | Call-output invariant with synthetic "aborted" | `NormalizeHistory()` (adopted) |
| **Ghost snapshots** | `ResponseItem::GhostSnapshot` in history | `GhostSnapshot` on disk (adopted, file-based) |
| **Rollback** | `drop_last_n_user_turns()` with fragment awareness | Not adopted (3-layer defense sufficient) |
| **Compaction strategies** | Manual / pre-sampling / mid-turn | Proactive (token threshold) / reactive (API rejection) / emergency flush |
| **Startup prewarm** | `SessionStartupPrewarmHandle` | `Prewarm()` with goroutines (adopted) |
| **Encrypted reasoning** | Base64-encoded with local estimation | Not adopted (model-specific) |
| **Session format** | In-memory `Vec<ResponseItem>` | JSONL files (different approach) |
| **Context fragments** | Auto-marked with rollback awareness | Not adopted (high complexity) |
| **Provider support** | OpenAI only | Multi-provider (Anthropic, OpenAI-compatible) |

### Key Differences

1. **Differential Strategy**: Codex diffs at the per-item level in conversation history and emits developer messages for changes. ycode diffs at the section level in the system prompt and omits unchanged dynamic sections. Both achieve token reduction but at different granularity.

2. **State Persistence**: Codex keeps ghost snapshots in-memory within the `Vec<ResponseItem>`. ycode persists them as JSON files on disk, aligning with its filesystem-first philosophy.

3. **Provider Scope**: Codex targets OpenAI's API exclusively. ycode supports multiple providers and adapts its optimization strategy based on provider capabilities (caching vs differential injection).

4. **Rollback vs Defense**: Codex provides explicit rollback (`drop_last_n_user_turns`) with fragment-aware trimming. ycode relies on its 3-layer defense (prune → compact → flush) without explicit rollback, which is simpler but less granular.

---

## 8. Features Adopted by ycode

| Feature | Adaptation |
|---------|------------|
| Differential context injection | `ContextBaseline` with per-section SHA-256 hashing; activated when `CachingSupported=false` |
| Provider capability detection | Static lookup (`DetectCapabilities`) + config override for caching support |
| Ghost snapshots | File-based `GhostSnapshot` saved before compaction, never sent to model |
| History normalization | `NormalizeHistory()` synthesizes missing results, removes orphans |
| Startup prewarming | `Prewarm()` runs instruction discovery and memory loading concurrently |

### Features Not Adopted

| Feature | Reason |
|---------|--------|
| Contextual fragment markers / multi-turn rollback | High complexity, marginal benefit given 3-layer defense |
| Encrypted reasoning items | Model-specific (OpenAI extended thinking), not relevant to multi-provider |
| Pre-sampling compaction strategies | Already covered by proactive token monitoring + 3-layer defense |
| In-memory history model | ycode uses JSONL for interoperability and persistence |

---

## 9. Key Design Insights

### 9.1 Differential as Default

Codex's `reference_context_item` pattern reveals a key insight: in long conversations, the system prompt is overwhelmingly static between turns. By treating "nothing changed" as the default and only emitting deltas, Codex saves significant tokens. This is especially valuable for providers without server-side prompt caching.

### 9.2 Invariant Enforcement as Robustness

The call-output pairing invariant (every tool call has a result) seems like a minor detail, but it prevents a class of subtle bugs: models receiving orphaned tool results from previous turns may hallucinate about actions they didn't take. Synthesizing "aborted" results makes the interrupted state explicit.

### 9.3 Ghost State for Recovery

Ghost snapshots demonstrate that some state is valuable to preserve across compaction but should never be shown to the model. This "hidden channel" pattern is useful for:
- Debugging what happened before compaction
- Restoring internal state after history rewriting
- Tracking metadata (token counts, timing) without consuming model attention

### 9.4 Atomic Compaction Lifecycle

Codex treats compaction as a proper API turn with full lifecycle events, not just a history rewrite. This means:
- Compaction can be monitored and logged
- Errors during compaction are handled gracefully
- The model's compaction summary is a real response, not a synthetic injection

### 9.5 Configuration-Driven Flexibility

`TurnContext` contains ~30 independently configurable fields. This allows Codex to adapt behavior per-turn: different models, different sandbox policies, different reasoning efforts, different personalities — all without restarting the session. ycode's `RuntimeContext` follows a similar pattern but at a coarser granularity.

---

## 10. File Reference

| Concept | Codex Source File |
|---------|------------------|
| Context manager (history + tokens) | `core/src/context_manager/history.rs` |
| Differential context updates | `core/src/context_manager/updates.rs` |
| History normalization | `core/src/context_manager/normalize.rs` |
| Session state | `core/src/state/session.rs` |
| Session + TurnContext structures | `core/src/codex.rs:824-910` |
| `build_initial_context()` | `core/src/codex.rs:3704-3850` |
| Compaction strategies | `core/src/compact.rs` |
| API request building | `core/src/client.rs` |
| Prompt assembly | `core/src/client_common.rs` |
| Instruction fragments | `core/src/instructions/src/fragment.rs` |
| Memory trace integration | `core/src/memory_trace.rs` |

---

*This analysis is based on the Codex CLI codebase (`x/codex/`) as of April 2026.*

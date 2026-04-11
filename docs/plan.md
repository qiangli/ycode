# ycode: Pure Go Rewrite of priorart/clawcode (Claw Code)

## Context

priorart/clawcode (Claw Code) is a Rust-based CLI agent harness (~66K LOC, 9 crates) for autonomous software development. It provides 50 tools, MCP/LSP integration, a plugin system, permission enforcement, multi-layered memory, and session management. The goal is to rewrite it in pure Go with only permissive-license dependencies, matching all features while improving on architecture. ycode should be a capable general-purpose agent, not limited to coding.

---

## Project Structure

```
ycode/
  go.mod
  cmd/ycode/main.go                    # Entry point, cobra root command

  internal/
    cli/                                # REPL, rendering, input
      app.go                           # Main app struct, run loop
      input.go                         # REPL input (bubbletea)
      render.go                        # Markdown + syntax highlighting
    api/                                # Provider clients
      client.go                        # Provider interface
      anthropic.go                     # Anthropic API client
      openai_compat.go                 # OpenAI-compatible client
      sse.go                           # SSE stream parser
      types.go                         # Request/response types
      prompt_cache.go                  # Prompt fingerprinting, TTL, cache stats
    runtime/
      config/                          # 3-tier config merge
      session/                         # JSONL persistence, compaction
        session.go                     # Session struct, JSONL read/write, rotation (256KB)
        message.go                     # MessageRole, ContentBlock, ConversationMessage
        compact.go                     # Auto-compaction at 100K tokens, semantic summary
        summary.go                     # Summary compression (1200 chars, 24 lines budget)
      conversation/                    # Turn loop, tool execution
      permission/                      # Modes, policy, enforcer
      memory/                          # Multi-layered memory system
        memory.go                      # Memory manager, auto-memory behaviors
        discovery.go                   # CLAUDE.md/MEMORY.md ancestry discovery
        types.go                       # MemoryType: episodic, semantic, working, user, feedback, project, reference
        store.go                       # File-based memory persistence (~/.ycode/projects/...)
        index.go                       # MEMORY.md index management
        search.go                      # Memory retrieval, relevance scoring
        age.go                         # Temporal memory decay, staleness detection
      mcp/                             # MCP client, stdio/ws transports
      lsp/                             # LSP client registry
      fileops/                         # read, write, edit, glob, grep
      bash/                            # Bash execution + validation
      prompt/                          # System prompt assembly
        builder.go                     # SystemPromptBuilder, section-based assembly
        sections.go                    # Intro, system, tasks, actions, environment sections
        context.go                     # ProjectContext, ContextFile
        discovery.go                   # Instruction file discovery (CWD to root ancestry)
        boundary.go                    # Dynamic boundary marker (static vs dynamic split)
      git/                             # Git context, branch detection
      hooks/                           # Hook runner
      sandbox/                         # Sandbox isolation
      oauth/                           # PKCE OAuth flow
      worker/                          # Worker registry + lifecycle
      task/                            # Task registry
      team/                            # Team + cron registries
      usage/                           # Token/cost tracking
      loop/                            # Continuous agent loop (Ralph pattern)
      scratchpad/                      # Markdown working memory (Manus pattern)
    tools/                              # Tool specs, registry, dispatch
      registry.go                      # Map-based tool registry
      specs.go                         # All 50 tool definitions
      deferred.go                      # Deferred tool loading, ToolSearch
      bash.go                          # bash, REPL, PowerShell
      file.go                          # read_file, write_file, edit_file
      search.go                        # glob_search, grep_search
      web.go                           # WebFetch, WebSearch
      agent.go                         # Agent (subagent spawning)
      task.go                          # TaskCreate/Get/List/Update/Stop/Output, RunTaskPacket
      worker.go                        # Worker* tools (9 tools)
      team.go                          # TeamCreate/Delete, CronCreate/Delete/List
      mcp_tools.go                     # MCP, ListMcpResources, ReadMcpResource, McpAuth
      lsp_tools.go                     # LSP tool
      notebook.go                      # NotebookEdit
      interaction.go                   # AskUserQuestion, SendUserMessage/Brief
      todo.go                          # TodoWrite
      skill.go                         # Skill loader
      config_tool.go                   # Config get/set
      mode.go                          # EnterPlanMode, ExitPlanMode
      structured.go                    # StructuredOutput
      sleep.go                         # Sleep
      remote.go                        # RemoteTrigger
    commands/                           # Slash commands (~30+)
    plugins/                            # Plugin manager, hooks
    telemetry/                          # Sinks, tracing
  pkg/ycode/                            # Public embedding API
```

## Dependencies (all permissive)

| Purpose | Library | License |
|---|---|---|
| CLI framework | `github.com/spf13/cobra` | Apache-2.0 |
| TUI/REPL | `github.com/charmbracelet/bubbletea` | MIT |
| Markdown | `github.com/charmbracelet/glamour` | MIT |
| Syntax highlight | `github.com/alecthomas/chroma/v2` | MIT |
| Terminal style | `github.com/charmbracelet/lipgloss` | MIT |
| UUID | `github.com/google/uuid` | BSD-3 |
| JSON-RPC (LSP) | `github.com/sourcegraph/jsonrpc2` | MIT |
| MCP | `github.com/modelcontextprotocol/go-sdk` | MIT |
| Everything else | Go stdlib | BSD |

## Key Design Decisions

1. **Map-based ToolRegistry** instead of Rust's giant `match` -- allows runtime registration from plugins/MCP
2. **`RuntimeContext` struct** holds all registries (no global state via `OnceLock`)
3. **`context.Context`** propagation everywhere for cancellation/timeout
4. **bubbletea** for REPL (unifies readline + terminal control)
5. **JSONL sessions** (keep priorart/clawcode format for potential interop)
6. **Single static binary** with cobra subcommands
7. **`pkg/ycode/`** for embedding as library (improvement over priorart/clawcode)
8. **`log/slog`** for structured logging
9. **Multi-layered memory** with file-based persistence and ancestry discovery
10. **Section-based prompt assembly** with dynamic boundary for cache optimization

## Improvements Over priorart/clawcode

- **Embeddable library API** via `pkg/ycode/`
- **No global state** -- all registries on `RuntimeContext`
- **Simpler concurrency** -- goroutines + channels vs tokio
- **Explicit error handling** -- sentinel errors, no panics
- **Runtime tool registration** -- plugins/MCP add tools without recompilation
- **Per-tool middleware** -- permission, logging, timing as composable wrappers
- **Full memory subsystem** -- episodic/semantic/working memory types (priorart/clawcode archived this from TS era, never ported to Rust)
- **Continuous agent loop (Ralph pattern)** -- real `/loop` command + background scheduler (priorart/clawcode has cron registry but no execution engine)
- **Markdown working memory (Manus pattern)** -- scratch pads, checkpoints, progress files on disk (priorart/clawcode has config flags but no implementation)
- **Recursive agent delegation** -- agents can spawn child agents for auto-research chains (priorart/clawcode explicitly blocks this)
- **Executable skills** -- skills can include Go scripts/plugins, not just markdown instructions

---

## Agent Patterns & Capabilities

### Ralph Pattern: Continuous Agent Loop

priorart/clawcode status: Infrastructure exists (CronRegistry, worker prompt replay) but no execution engine.

ycode implements a real continuous loop system:
```
internal/runtime/loop/
  loop.go        # LoopController: start/stop/pause continuous execution
  scheduler.go   # Background scheduler with cron expression support
  watcher.go     # File watcher for prompt file changes (fsnotify)
```

- `/loop [interval] [command]` slash command (e.g., `/loop 5m /review`)
- `ycode loop --interval 5m --prompt prompt.md` CLI mode
- Reads prompt from file, executes agent, captures output, waits, repeats
- Context carryover between iterations via session continuation
- Improvement metrics tracking across iterations
- Graceful shutdown via context cancellation

### Manus Pattern: Markdown Working Memory

priorart/clawcode status: `autoMemoryEnabled`, `autoDreamEnabled`, `fileCheckpointingEnabled` flags exist but are unimplemented.

ycode implements real disk-based working memory:
```
internal/runtime/scratchpad/
  scratchpad.go  # ScratchpadManager: create/read/update/delete scratch files
  checkpoint.go  # Checkpoint manager: save/restore progress snapshots
  worklog.go     # Append-only work log for narrative tracking
```

- `.ycode/scratchpad/` directory for per-session scratch files
- `.ycode/checkpoints/` for progress snapshots (restorable)
- `.ycode/worklog.md` for append-only narrative of agent activity
- Auto-checkpoint on compaction (fileCheckpointingEnabled)
- Auto-dream: background memory consolidation across sessions (autoDreamEnabled)
- Agent output files: `{id}.md` (narrative) + `{id}.json` (structured manifest)

### Auto-Agent / Auto-Research

priorart/clawcode status: Agents cannot spawn child agents (explicitly blocked in allowed_tools_for_subagent).

ycode allows controlled recursive delegation:
- Agents can spawn child agents up to configurable depth (default: 3)
- Auto-research mode: agent autonomously breaks down research into sub-tasks
- Self-configuring: agents use Config tool to adapt behavior to project context
- `/advisor` and `/insights` can auto-trigger analysis (not just manual slash commands)

### Skills System

priorart/clawcode status: Hierarchical filesystem discovery, markdown-only, no executable code.

ycode extends skills:
```
~/.ycode/skills/{skillname}/
  SKILL.md           # Instructions (markdown with YAML frontmatter)
  scripts/           # Optional executable scripts (Go plugins, shell scripts)
  resources/         # Data files, templates, examples
  tests/             # Skill-specific test cases
```

- Same discovery chain as priorart/clawcode (project ancestors → home → env vars)
- YAML frontmatter for metadata (name, description, triggers, dependencies)
- Executable scripts: shell scripts or compiled Go plugins
- Skill dependencies: skills can reference other skills
- Bundled skills: remember, loop, simplify, review, commit, pr (ported from priorart/clawcode TS archive)
- `/skills` slash command for discovery and management

---

## Memory System Architecture

ycode implements a multi-layered memory system:

```
┌─────────────────────────────────────────────────────┐
│              Current Conversation Turn               │
│  (ConversationRuntime holds active Session)          │
└────────────┬────────────────────────────────────────┘
             │
             ├─ Working Memory (Context Window)
             │  └─ All messages in current session
             │  └─ System prompt (with instruction files)
             │  └─ Tool results, pending operations
             │
             ├─ Short-term Memory (Session Persistence)
             │  └─ JSONL file: ~/.local/share/ycode/sessions/
             │  └─ Rotates after 256KB, keeps 3 rotated files
             │  └─ Tracks message history + prompt timeline
             │  └─ Session resume via --resume [id|latest]
             │
             ├─ Long-term Memory (Session Compaction)
             │  └─ Triggered at 100K input tokens (configurable)
             │  └─ Preserves last 4 messages verbatim
             │  └─ Semantic summary extraction:
             │     - Message scope (user/assistant/tool counts)
             │     - Tools mentioned (deduplicated)
             │     - Recent user requests (last 3, 160 chars each)
             │     - Pending work (lines with "todo", "next", "pending")
             │     - Key files referenced (up to 8)
             │     - Current work status (200 chars)
             │     - Key timeline (role-labeled recap)
             │  └─ Summary compression: 1,200 chars, 24 lines max
             │  └─ Multi-compaction: merges previous + new summaries
             │
             ├─ Contextual Memory (Instruction Files)
             │  └─ Discovered from CWD → root ancestor chain
             │  └─ Files: CLAUDE.md, CLAUDE.local.md, .ycode/CLAUDE.md
             │  └─ Per-file budget: 4,000 chars, total: 12,000 chars
             │  └─ Deduplicated by content hash
             │  └─ Injected into system prompt after dynamic boundary
             │
             ├─ Persistent Memory (File-based)
             │  └─ ~/.ycode/projects/{project}/memory/
             │  └─ MEMORY.md index (<200 lines, one-line entries)
             │  └─ Individual memory files with frontmatter:
             │     - type: user | feedback | project | reference
             │     - name, description for relevance matching
             │  └─ Auto-memory: save/recall triggered by config flag
             │
             └─ Extended Memory (Planned)
                └─ Episodic memory: event sequences, what happened when
                └─ Semantic memory: facts, concepts, relationships
                └─ Embedding-based retrieval for large memory stores
                └─ Memory aging and decay policies
```

### Memory-related Config Settings
- `autoMemoryEnabled` -- automatically save/recall memories
- `autoDreamEnabled` -- background memory consolidation
- `autoCompactEnabled` -- auto-trigger compaction at token threshold

---

## Prompt System Architecture

System prompt is assembled in sections via `SystemPromptBuilder`:

```
┌─────────── STATIC SECTIONS (cacheable) ───────────┐
│ 1. Intro: Role description ("You are an agent...") │
│ 2. Output style (if configured)                     │
│ 3. System: Tool usage, permissions, compression     │
│ 4. Doing tasks: Code quality, scope, safety         │
│ 5. Actions: Reversibility, blast radius guidance    │
├─── __SYSTEM_PROMPT_DYNAMIC_BOUNDARY__ ──────────────┤
│ 6. Environment: Model family, CWD, date, platform  │
│ 7. Project context: Discovered instruction file cnt │
│ 8. Git context: Branch, recent commits, staged      │
│ 9. Git diff snapshot: Staged + unstaged changes     │
│ 10. Instruction files: CLAUDE.md ancestry chain     │
│ 11. Memory context: Relevant recalled memories      │
│ 12. Runtime config section                          │
│ 13. Custom appended sections                        │
└─────────────────────────────────────────────────────┘
```

### Prompt Caching
- Fingerprints: model_hash, system_hash, tools_hash, messages_hash
- Completion cache TTL: 30 seconds
- Prompt cache TTL: 5 minutes
- Tracks cache hits/misses/writes
- Detects unexpected cache breaks (token drop > 2,000 without prompt change)

### Agent/Subagent Prompt Construction
Each subagent type gets a tailored prompt + tool allowlist:
- **Explore**: read-only tools (read_file, glob, grep, WebFetch, WebSearch, ToolSearch, Skill)
- **Plan**: Explore + TodoWrite + SendUserMessage
- **Verification**: Plan + bash, write_file, edit_file, REPL, PowerShell
- **general-purpose**: All common tools
- **claw-guide**: Read-only + messaging
- **statusline-setup**: Specialized config tools

---

## Complete Tool Catalog (50 tools)

### Core File Operations (6)
| Tool | Permission | Description |
|------|-----------|-------------|
| bash | DangerFullAccess | Execute shell command (timeout, background, sandbox) |
| read_file | ReadOnly | Read text file (offset, limit) |
| write_file | WorkspaceWrite | Write text file |
| edit_file | WorkspaceWrite | Replace text in file (old_string → new_string, replace_all) |
| glob_search | ReadOnly | Find files by glob pattern |
| grep_search | ReadOnly | Search file contents with regex (context, multiline, type filter) |

### Web Tools (2)
| Tool | Permission | Description |
|------|-----------|-------------|
| WebFetch | ReadOnly | Fetch URL, convert to text, answer prompt about it |
| WebSearch | ReadOnly | Search web via DuckDuckGo, return cited results |

### Interaction & Workflow (4)
| Tool | Permission | Description |
|------|-----------|-------------|
| TodoWrite | WorkspaceWrite | Update structured task list (pending/in_progress/completed) |
| Skill | ReadOnly | Load skill definition from ancestry chain |
| AskUserQuestion | ReadOnly | Ask user a question (with optional multiple choice) |
| SendUserMessage/Brief | ReadOnly | Send message to user |

### Agent & Task Management (8)
| Tool | Permission | Description |
|------|-----------|-------------|
| Agent | DangerFullAccess | Spawn subagent with type-specific tools + prompt |
| TaskCreate | DangerFullAccess | Create background task |
| RunTaskPacket | DangerFullAccess | Create task from structured packet |
| TaskGet | ReadOnly | Get task status |
| TaskList | ReadOnly | List all tasks |
| TaskUpdate | DangerFullAccess | Send message to running task |
| TaskStop | DangerFullAccess | Stop running task |
| TaskOutput | ReadOnly | Get task output |

### Worker Tools (9)
| Tool | Permission | Description |
|------|-----------|-------------|
| WorkerCreate | DangerFullAccess | Create worker with trust gate |
| WorkerGet | ReadOnly | Get worker boot state |
| WorkerObserve | ReadOnly | Feed terminal snapshot for boot detection |
| WorkerResolveTrust | DangerFullAccess | Resolve trust gate |
| WorkerAwaitReady | ReadOnly | Wait for ready-for-prompt state |
| WorkerSendPrompt | DangerFullAccess | Deliver prompt to worker |
| WorkerRestart | DangerFullAccess | Restart worker |
| WorkerTerminate | DangerFullAccess | Terminate worker |
| WorkerObserveCompletion | ReadOnly | Check worker completion |

### Team & Scheduling (5)
| Tool | Permission | Description |
|------|-----------|-------------|
| TeamCreate | DangerFullAccess | Create team of parallel sub-agents |
| TeamDelete | DangerFullAccess | Delete team and stop tasks |
| CronCreate | DangerFullAccess | Create scheduled recurring task |
| CronDelete | DangerFullAccess | Delete scheduled task |
| CronList | ReadOnly | List scheduled tasks |

### Code Intelligence (2)
| Tool | Permission | Description |
|------|-----------|-------------|
| LSP | ReadOnly | Query language server (symbols, refs, diagnostics, hover, definition) |
| NotebookEdit | WorkspaceWrite | Edit Jupyter notebook cells (replace, insert, delete) |

### External Integration (5)
| Tool | Permission | Description |
|------|-----------|-------------|
| MCP | DangerFullAccess | Execute MCP server tool |
| ListMcpResources | ReadOnly | List MCP server resources |
| ReadMcpResource | ReadOnly | Read MCP resource by URI |
| McpAuth | DangerFullAccess | Authenticate with MCP server |
| RemoteTrigger | DangerFullAccess | Trigger remote webhook/endpoint |

### Configuration & Mode (3)
| Tool | Permission | Description |
|------|-----------|-------------|
| Config | WorkspaceWrite | Get/set runtime settings |
| EnterPlanMode | WorkspaceWrite | Enable planning mode |
| ExitPlanMode | WorkspaceWrite | Exit planning mode |

### Output & Execution (4)
| Tool | Permission | Description |
|------|-----------|-------------|
| Sleep | ReadOnly | Wait for duration (ms) |
| REPL | DangerFullAccess | Execute code in REPL subprocess (Python, JS, Go, etc.) |
| PowerShell | DangerFullAccess | Execute PowerShell command |
| StructuredOutput | ReadOnly | Return structured JSON output |

### Utility (2)
| Tool | Permission | Description |
|------|-----------|-------------|
| ToolSearch | ReadOnly | Search deferred tools by name/keyword (scoring algorithm) |
| TestingPermission | DangerFullAccess | Test-only permission verification |

### Deferred vs Always-Available
- **Always available** (sent in every request): bash, read_file, write_file, edit_file, glob_search, grep_search
- **Deferred** (discovered via ToolSearch or on-demand): All other tools

---

## Implementation Phases

### Phase 1: Foundation
1. Go module init, directory scaffolding, cobra CLI skeleton
2. `internal/api/` -- Anthropic client, SSE parser, streaming via channels
3. `internal/runtime/session/` -- JSONL persistence, ContentBlock, MessageRole
4. `internal/runtime/config/` -- ConfigLoader, 3-tier merge
5. `internal/runtime/permission/` -- PermissionMode, basic policy
6. `internal/cli/` -- REPL with bubbletea, stream rendering with glamour + chroma

### Phase 2: Core Tools & Prompt
7. `internal/runtime/fileops/` -- read, write, edit, glob, grep
8. `internal/runtime/bash/` -- execute with timeout, background, validation
9. `internal/tools/` -- ToolSpec, ToolRegistry, dispatch, all 50 tools
10. `internal/runtime/conversation/` -- turn loop, auto-compaction
11. `internal/runtime/prompt/` -- Full prompt assembly pipeline (sections, boundary, discovery)
12. `internal/runtime/usage/` -- token tracking, cost estimation

### Phase 3: Memory & Context
13. `internal/runtime/memory/` -- Memory manager, file-based persistence, MEMORY.md index
14. `internal/runtime/memory/` -- Discovery (CLAUDE.md ancestry), types, search, aging
15. `internal/runtime/session/compact.go` -- Semantic compaction, summary compression
16. `internal/api/prompt_cache.go` -- Prompt fingerprinting, cache stats, TTL tracking
17. Memory integration into prompt builder and conversation loop

### Phase 4: Commands & Plugins
18. `internal/commands/` -- all 30+ slash commands
19. `internal/plugins/` -- PluginManager, hook execution, discovery
20. `internal/runtime/hooks/` -- HookRunner with pre/post tool hooks
21. One-shot mode, piped input, `--print` flag

### Phase 5: Advanced Features
22. `internal/runtime/mcp/` -- MCP client/server using go-sdk, stdio/SSE/WS transports
23. `internal/runtime/lsp/` -- LSP client registry and actions
24. `internal/runtime/worker/` -- Worker boot lifecycle, trust gates
25. `internal/runtime/task/` -- TaskRegistry
26. `internal/runtime/team/` -- Team + Cron registries
27. `internal/runtime/oauth/` -- PKCE OAuth flow
28. `internal/runtime/git/` -- Git context, branch lock, stale detection

### Phase 6: Polish & Embedding
29. `pkg/ycode/` -- Public API with functional options
30. `internal/runtime/sandbox/` -- Container detection, Linux namespace sandbox
31. OpenAI-compatible provider support
32. Cross-compilation, `-ldflags` version injection

### Phase 7: Testing & Hardening
33. Mock Anthropic service for integration tests
34. Port priorart/clawcode's 10 parity test scenarios
35. Fuzz tests (SSE parser, JSON-RPC, config)
36. Race detection in CI (`go test -race`)
37. Documentation

## Reference Files (priorart/clawcode)

- `priorart/clawcode/rust/crates/runtime/src/lib.rs` -- runtime module structure
- `priorart/clawcode/rust/crates/tools/src/lib.rs` -- all 50 tool specs + dispatch (8607 lines)
- `priorart/clawcode/rust/crates/rusty-claude-cli/src/main.rs` -- CLI entry point
- `priorart/clawcode/rust/crates/api/src/client.rs` -- provider client dispatch
- `priorart/clawcode/rust/crates/api/src/prompt_cache.rs` -- prompt caching implementation
- `priorart/clawcode/rust/crates/runtime/src/conversation.rs` -- turn loop, ApiRequest assembly
- `priorart/clawcode/rust/crates/runtime/src/prompt.rs` -- system prompt builder (905 lines)
- `priorart/clawcode/rust/crates/runtime/src/compact.rs` -- compaction logic, semantic summary
- `priorart/clawcode/rust/crates/runtime/src/summary_compression.rs` -- summary budget enforcement
- `priorart/clawcode/rust/crates/runtime/src/session.rs` -- session struct, JSONL persistence
- `priorart/clawcode/rust/crates/runtime/src/git_context.rs` -- git context discovery
- `priorart/clawcode/rust/crates/runtime/src/permissions.rs` -- permission policy
- `priorart/clawcode/rust/crates/runtime/src/hooks.rs` -- hook system
- `priorart/clawcode/rust/crates/runtime/src/mcp_tool_bridge.rs` -- MCP tool bridge
- `priorart/clawcode/src/reference_data/subsystems/memdir.json` -- archived memory subsystem design

## Verification

1. `go build ./cmd/ycode/` compiles a single static binary
2. `ycode doctor` passes health checks
3. `ycode prompt "hello"` completes a one-shot interaction
4. Interactive REPL renders markdown with syntax highlighting
5. File tools (read, write, edit, glob, grep) work against test fixtures
6. Bash tool executes commands with timeout and permission enforcement
7. Session persists and resumes from JSONL
8. Memory saves/recalls across conversations
9. Compaction triggers at token threshold and produces valid summary
10. Prompt caching tracks fingerprints and detects cache breaks
11. `go test -race ./...` passes all tests

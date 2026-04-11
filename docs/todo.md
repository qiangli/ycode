# ycode Implementation Checklist

Track progress here. Check off items as they are completed.

## Phase 1: Foundation
- [x] 1.1 Go module init (`go mod init`), directory scaffolding, `.gitignore`
- [x] 1.2 Cobra CLI skeleton (`cmd/ycode/main.go`, root + subcommands)
- [x] 1.3 `internal/api/types.go` -- request/response types, ContentBlock, MessageRole, ToolDefinition
- [x] 1.4 `internal/api/sse.go` -- SSE stream parser (event: data: field parsing, buffered reader)
- [x] 1.5 `internal/api/anthropic.go` -- Anthropic API client with streaming via channels
- [x] 1.6 `internal/api/client.go` -- Provider interface (Send, Kind)
- [x] 1.7 `internal/runtime/session/session.go` -- Session struct, JSONL read/write, rotation at 256KB
- [x] 1.8 `internal/runtime/session/message.go` -- MessageRole, ContentBlock, ConversationMessage
- [x] 1.9 `internal/runtime/config/config.go` -- ConfigLoader, 3-tier merge (user > project > local)
- [x] 1.10 `internal/runtime/permission/mode.go` -- PermissionMode enum (ReadOnly, WorkspaceWrite, DangerFullAccess)
- [x] 1.11 `internal/runtime/permission/policy.go` -- PermissionPolicy, rule matching (allow/deny/ask)
- [x] 1.12 `internal/runtime/permission/enforcer.go` -- PermissionEnforcer, PermissionPrompter interface
- [x] 1.13 `internal/cli/render.go` -- Markdown rendering (glamour) + syntax highlighting (chroma)
- [x] 1.14 `internal/cli/input.go` -- REPL input handling (bubbletea)
- [x] 1.15 `internal/cli/app.go` -- Main app struct, interactive run loop
- [x] 1.16 Phase 1 integration: `ycode prompt "hello"` works end-to-end

## Phase 2: Core Tools & Prompt System
- [x] 2.1 `internal/tools/registry.go` -- ToolSpec, ToolRegistry, map-based dispatch, per-tool middleware
- [x] 2.2 `internal/tools/types.go` -- RuntimeToolDefinition, ToolSource, ToolFunc, deferred vs always-available
- [x] 2.3 `internal/runtime/fileops/read.go` -- read_file (offset, limit, binary detection, size limits)
- [x] 2.4 `internal/runtime/fileops/write.go` -- write_file (workspace boundary, size limits)
- [x] 2.5 `internal/runtime/fileops/edit.go` -- edit_file (old_string→new_string, replace_all, uniqueness check)
- [x] 2.6 `internal/runtime/fileops/glob.go` -- glob_search (filepath.WalkDir, pattern matching)
- [x] 2.7 `internal/runtime/fileops/grep.go` -- grep_search (regex, context lines, output modes, type filter)
- [x] 2.8 `internal/runtime/bash/exec.go` -- Bash execution (timeout, background, namespace, sandbox)
- [x] 2.9 `internal/runtime/bash/validate.go` -- Read-only mode, destructive command warnings, path validation
- [x] 2.10 `internal/tools/specs.go` -- All 50 tool spec definitions with JSON schemas
- [x] 2.11 `internal/tools/bash.go` -- bash, REPL, PowerShell handlers
- [x] 2.12 `internal/tools/file.go` -- read_file, write_file, edit_file handlers
- [x] 2.13 `internal/tools/search.go` -- glob_search, grep_search handlers
- [x] 2.14 `internal/tools/web.go` -- WebFetch (HTML→text, prompt analysis), WebSearch (DuckDuckGo)
- [x] 2.15 `internal/tools/interaction.go` -- AskUserQuestion, SendUserMessage/Brief
- [x] 2.16 `internal/tools/todo.go` -- TodoWrite (.clawd-todos.json persistence, verification nudge)
- [x] 2.17 `internal/tools/skill.go` -- Skill loader (ancestry search, YAML frontmatter parsing)
- [x] 2.18 `internal/tools/deferred.go` -- ToolSearch (scoring: exact +12, name +8, contains +4, haystack +2)
- [x] 2.19 `internal/tools/config_tool.go` -- Config get/set
- [x] 2.20 `internal/tools/mode.go` -- EnterPlanMode, ExitPlanMode
- [x] 2.21 `internal/tools/structured.go` -- StructuredOutput
- [x] 2.22 `internal/tools/sleep.go` -- Sleep (duration_ms)
- [x] 2.23 `internal/tools/remote.go` -- RemoteTrigger (HTTP methods, 30s timeout, 8KB body truncation)
- [x] 2.24 `internal/tools/notebook.go` -- NotebookEdit (replace, insert, delete cells)
- [x] 2.25 `internal/runtime/prompt/builder.go` -- SystemPromptBuilder, section-based assembly
- [x] 2.26 `internal/runtime/prompt/sections.go` -- Intro, system, tasks, actions section content
- [x] 2.27 `internal/runtime/prompt/context.go` -- ProjectContext, ContextFile structs
- [x] 2.28 `internal/runtime/prompt/discovery.go` -- Instruction file discovery (CWD→root, dedup by hash)
- [x] 2.29 `internal/runtime/prompt/boundary.go` -- Dynamic boundary marker, static/dynamic split
- [x] 2.30 `internal/runtime/conversation/runtime.go` -- ConversationRuntime, turn loop, ApiRequest assembly
- [x] 2.31 `internal/runtime/conversation/executor.go` -- ToolExecutor interface + SubagentToolExecutor
- [x] 2.32 `internal/runtime/usage/tracker.go` -- Token counting (input, output, cache_creation, cache_read), cost
- [x] 2.33 Phase 2 integration: interactive REPL with tool use and prompt assembly works

## Phase 3: Memory System
- [x] 3.1 `internal/runtime/memory/types.go` -- MemoryType enum (user, feedback, project, reference), Memory struct with frontmatter
- [x] 3.2 `internal/runtime/memory/store.go` -- File-based persistence (~/.ycode/projects/{hash}/memory/)
- [x] 3.3 `internal/runtime/memory/index.go` -- MEMORY.md index management (<200 lines, one-line entries)
- [x] 3.4 `internal/runtime/memory/discovery.go` -- CLAUDE.md/MEMORY.md ancestry discovery from CWD to root
- [x] 3.5 `internal/runtime/memory/search.go` -- Memory retrieval, relevance scoring by description
- [x] 3.6 `internal/runtime/memory/age.go` -- Temporal memory decay, staleness detection
- [x] 3.7 `internal/runtime/memory/memory.go` -- MemoryManager: save, recall, forget, auto-memory behaviors
- [x] 3.8 `internal/runtime/session/compact.go` -- Auto-compaction at 100K tokens, preserve last 4 messages
- [x] 3.9 `internal/runtime/session/summary.go` -- Semantic summary extraction (scope, tools, requests, pending, files, timeline)
- [x] 3.10 `internal/runtime/session/compression.go` -- Summary compression (1200 chars, 24 lines, 160 chars/line budget)
- [x] 3.11 `internal/api/prompt_cache.go` -- Prompt fingerprinting (model, system, tools, messages hashes)
- [x] 3.12 `internal/api/prompt_cache.go` -- Cache TTL (completion 30s, prompt 5min), break detection (>2K token drop)
- [x] 3.13 Memory integration: inject recalled memories into prompt builder after dynamic boundary
- [x] 3.14 Memory integration: auto-save from conversation when `autoMemoryEnabled` is true
- [x] 3.15 Phase 3 integration: memory persists across conversations, compaction produces valid summaries

## Phase 4: Commands & Plugins
- [x] 4.1 `internal/commands/registry.go` -- SlashCommandSpec, command dispatch
- [x] 4.2 `internal/commands/handlers.go` -- Session commands: /help, /status, /cost, /resume, /version, /usage
- [x] 4.3 `internal/commands/handlers.go` -- Workspace commands: /compact, /clear, /config, /memory, /init, /diff, /commit, /pr
- [x] 4.4 `internal/commands/handlers.go` -- Discovery commands: /mcp, /agents, /skills, /doctor, /tasks, /context
- [x] 4.5 `internal/commands/handlers.go` -- Automation commands: /review, /advisor, /security-review, /team, /cron
- [x] 4.6 `internal/commands/handlers.go` -- Plugin commands: /plugin list|install|enable|disable|uninstall|update
- [x] 4.7 `internal/plugins/manager.go` -- PluginManager, install/enable/disable/uninstall
- [x] 4.8 `internal/plugins/registry.go` -- Plugin discovery, manifest parsing, PluginKind (builtin, bundled, external)
- [x] 4.9 `internal/plugins/hooks.go` -- Plugin hook execution (PreToolUse, PostToolUse, PostToolUseFailure)
- [x] 4.10 `internal/runtime/hooks/runner.go` -- HookRunner, HookEvent, permission overrides from hooks
- [x] 4.11 One-shot mode (`ycode prompt "..."`) with streaming output
- [x] 4.12 Piped input mode + `--print` flag
- [x] 4.13 Phase 4 integration: slash commands and plugins work

## Phase 5: Agent Patterns (Ralph, Manus, Auto-Agent, Skills)
- [x] 5.1 `internal/runtime/loop/loop.go` -- LoopController: start/stop/pause continuous agent execution
- [x] 5.2 `internal/runtime/loop/scheduler.go` -- Background scheduler with cron expression support
- [x] 5.3 `internal/runtime/loop/watcher.go` -- File watcher for prompt file changes
- [x] 5.4 `/loop` slash command (`/loop 5m /review`) and `ycode loop` CLI subcommand
- [x] 5.5 Loop context carryover: session continuation between iterations, improvement metrics
- [x] 5.6 `internal/runtime/scratchpad/scratchpad.go` -- ScratchpadManager: create/read/update/delete scratch .md files
- [x] 5.7 `internal/runtime/scratchpad/checkpoint.go` -- Checkpoint manager: save/restore progress snapshots
- [x] 5.8 `internal/runtime/scratchpad/worklog.go` -- Append-only work log for narrative tracking
- [x] 5.9 Auto-checkpoint on compaction (`fileCheckpointingEnabled` config)
- [x] 5.10 Auto-dream: background memory consolidation across sessions (`autoDreamEnabled` config)
- [x] 5.11 Agent recursive delegation: allow child agent spawning up to configurable depth (default: 3)
- [x] 5.12 Auto-research mode: agent autonomously breaks research into sub-tasks
- [x] 5.13 Skills system: hierarchical discovery (project ancestors → home → env vars)
- [x] 5.14 Skills YAML frontmatter parsing, case-insensitive name matching
- [x] 5.15 Skills executable scripts: shell scripts and Go plugin support in `scripts/` subdirectory
- [x] 5.16 Skills resources: data files, templates, examples in `resources/` subdirectory
- [x] 5.17 Bundled skills: remember, loop, simplify, review, commit, pr (port from x/claw-code TS archive)
- [x] 5.18 `/skills` slash command for skill discovery and management
- [x] 5.19 Phase 5 integration: loop runs continuously, scratchpad persists, skills load and execute

## Phase 6: Infrastructure (MCP, LSP, Workers, Tasks, Teams, Git, OAuth)
- [x] 6.1 `internal/runtime/mcp/client.go` -- MCP client using `github.com/modelcontextprotocol/go-sdk`
- [x] 6.2 `internal/runtime/mcp/stdio.go` -- Stdio transport, process spawn, JSON-RPC framing
- [x] 6.3 `internal/runtime/mcp/server.go` -- MCP server mode (ycode as MCP server)
- [x] 6.4 `internal/runtime/mcp/bridge.go` -- MCP tool bridge, tool discovery, name normalization (mcp__{server}__{tool})
- [x] 6.5 `internal/runtime/mcp/lifecycle.go` -- Lifecycle validation, degraded mode reporting
- [x] 6.6 `internal/tools/mcp_tools.go` -- MCP, ListMcpResources, ReadMcpResource, McpAuth handlers
- [x] 6.7 `internal/runtime/lsp/client.go` -- LSP client, JSON-RPC 2.0, server process management
- [x] 6.8 `internal/runtime/lsp/types.go` -- LspAction, LspDiagnostic, LspLocation, LspSymbol
- [x] 6.9 `internal/runtime/lsp/actions.go` -- hover, definition, references, symbols, diagnostics
- [x] 6.10 `internal/tools/lsp_tools.go` -- LSP tool handler
- [x] 6.11 `internal/runtime/worker/worker.go` -- Worker struct, WorkerRegistry
- [x] 6.12 `internal/runtime/worker/boot.go` -- Boot lifecycle (Spawning→TrustRequired→ReadyForPrompt→Running→Finished/Failed)
- [x] 6.13 `internal/runtime/worker/events.go` -- WorkerEvent types, trust resolution, prompt misdelivery recovery
- [x] 6.14 `internal/tools/worker.go` -- All 9 Worker* tool handlers
- [x] 6.15 `internal/runtime/task/registry.go` -- TaskRegistry, TaskPacket, create/get/list/update/stop/output
- [x] 6.16 `internal/tools/task.go` -- Task tool handlers + RunTaskPacket
- [x] 6.17 `internal/runtime/team/team.go` -- TeamRegistry
- [x] 6.18 `internal/runtime/team/cron.go` -- CronRegistry (with real execution engine, not just metadata)
- [x] 6.19 `internal/tools/team.go` -- TeamCreate/Delete, CronCreate/Delete/List handlers
- [x] 6.20 `internal/tools/agent.go` -- Agent tool (subagent spawning, type-based tool allowlists, manifest persistence)
- [x] 6.21 `internal/runtime/oauth/oauth.go` -- PKCE OAuth flow, token exchange, credential storage
- [x] 6.22 `internal/runtime/git/context.go` -- GitContext (branch, commits, staged files via git commands)
- [x] 6.23 `internal/runtime/git/branch_lock.go` -- Branch lock collision detection
- [x] 6.24 `internal/runtime/git/stale.go` -- Stale branch/base detection
- [x] 6.25 Phase 6 integration: MCP servers connect, LSP works, workers boot, agents spawn

## Phase 7: Polish & Embedding
- [x] 7.1 `pkg/ycode/ycode.go` -- Public API: NewAgent, Run, functional options
- [x] 7.2 `internal/runtime/sandbox/sandbox.go` -- Container detection, Linux namespace sandbox
- [x] 7.3 `internal/api/openai_compat.go` -- OpenAI-compatible provider (xAI, OpenAI, DashScope)
- [x] 7.4 `internal/runtime/policy/engine.go` -- Policy engine, lane events
- [x] 7.5 `internal/runtime/recovery/recipes.go` -- Recovery recipes, attempt_recovery
- [x] 7.6 `internal/telemetry/sink.go` -- TelemetrySink interface, JSONL sink, memory sink
- [x] 7.7 `internal/telemetry/tracer.go` -- SessionTracer, SessionTraceRecord
- [x] 7.8 `internal/telemetry/events.go` -- AnalyticsEvent, PromptCacheEvent
- [x] 7.9 Cross-compilation targets (Linux, macOS, Windows), `-ldflags` version injection
- [x] 7.10 `ycode doctor` health check command
- [x] 7.11 `ycode login/logout` OAuth commands
- [x] 7.12 Phase 7 integration: embedding API works, all providers supported

## Phase 8: Testing & Hardening
- [x] 8.1 `internal/testutil/mockapi/server.go` -- Mock Anthropic service (deterministic responses)
- [x] 8.2 Port x/claw-code's 10 parity test scenarios (streaming, file roundtrip, bash, permissions, plugins)
- [x] 8.3 Unit tests for all packages (target >80% coverage)
- [x] 8.4 Memory system tests: save/recall/forget, ancestry discovery, staleness decay
- [x] 8.5 Compaction tests: trigger threshold, summary extraction, multi-compaction merge
- [x] 8.6 Prompt assembly tests: section ordering, boundary marker, instruction file budgets
- [x] 8.7 Prompt cache tests: fingerprinting, TTL expiry, break detection
- [x] 8.8 Loop/scheduler tests: continuous execution, context carryover, graceful shutdown
- [x] 8.9 Scratchpad tests: checkpoint save/restore, worklog append, auto-checkpoint on compaction
- [x] 8.10 Skills tests: discovery chain, frontmatter parsing, executable script loading
- [x] 8.11 Fuzz tests: SSE parser, JSON-RPC, config loading, JSONL session parsing
- [x] 8.12 `go test -race ./...` passes
- [x] 8.13 `go vet ./...` clean
- [x] 8.14 CI/CD pipeline (GitHub Actions)
- [x] 8.15 README.md, USAGE.md documentation
- [x] 8.16 Final end-to-end validation against all verification steps

## Phase 9: Best-in-Class Memory & Context Management
- [x] 9.1 Context pruning (Layer 1): soft trim + hard clear of old tool results (`session/pruning.go`)
- [x] 9.2 Context health monitoring: token estimation, level tracking (healthy/warning/critical/overflow)
- [x] 9.3 Proactive auto-compaction: trigger at 100K threshold before API rejection
- [x] 9.4 Post-compaction context refresh: re-inject critical CLAUDE.md sections (`prompt/refresh.go`)
- [x] 9.5 Emergency memory flush (Layer 3): minimal continuation with summary + last user message
- [x] 9.6 3-layer defense integration in TurnWithRecovery (prune → compact → flush)
- [x] 9.7 User-visible context management notifications in CLI (prune/compact/flush indicators)
- [x] 9.8 Tests for pruning, context health, and post-compaction refresh
- [x] 9.9 Verify docs: memory-clawcode.md (accurate), memory-openclaw.md (1 fix), memory-opencode.md (accurate)
- [x] 9.10 Full build + test suite passes

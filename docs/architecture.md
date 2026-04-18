# ycode Architecture

ycode is a pure Go CLI agent harness for autonomous software development. It provides 50+ tools, MCP/LSP integration, a plugin system, permission enforcement, multi-layered memory, and session management. Single static binary, no external process dependencies.

---

## Project Layout

```
ycode/
  cmd/ycode/           CLI entry point (cobra commands, serve mode, OTEL wiring)
  internal/
    api/               Provider abstraction (Anthropic, OpenAI-compat), SSE, prompt cache
    cli/               Interactive REPL (bubbletea), markdown rendering (glamour + chroma)
    collector/         Embedded OpenTelemetry Collector (in-process)
    commands/          30+ slash commands (/help, /config, /review, /skills, ...)
    observability/     Embedded observability stack (Prometheus, Jaeger, VictoriaLogs, Perses)
    plugins/           Plugin manager, lifecycle hooks, discovery
    runtime/
      bash/            Shell execution with timeout, background, sandbox
      config/          3-tier config merge (user > project > local)
      conversation/    Turn loop, tool execution, loop detection, token recovery
      embedding/       Vector embeddings for semantic search
      fileops/         read, write, edit, glob, grep with VFS boundary enforcement
      git/             Git context discovery, branch lock, stale detection
      hooks/           Pre/post tool hooks
      indexer/         Background codebase indexing
      loop/            Continuous agent loop with cron scheduling
      lsp/             Language Server Protocol client
      mcp/             Model Context Protocol client/server, stdio/SSE transports
      memory/          Multi-layered memory (5 layers, 4 types)
      net/             Web fetch, web search
      oauth/           PKCE OAuth flow
      permission/      Mode enforcement (ReadOnly, WorkspaceWrite, DangerFullAccess)
      policy/          Permission policy rules
      prompt/          System prompt assembly (section-based, cache-optimized)
      recovery/        Error recovery recipes
      sandbox/         Container detection, namespace isolation
      scratchpad/      Markdown working memory, checkpoints, work log
      session/         JSONL persistence, compaction, semantic summaries
      task/            Task registry
      taskqueue/       Async task queue
      team/            Team + cron registries
      usage/           Token counting, cost estimation
      vfs/             Virtual filesystem with allowed-directory boundaries
      worker/          Worker boot lifecycle, trust gates
    selfheal/          Self-healing wrapper (error classification, auto-rebuild)
    storage/           Persistence layer (KV, SQLite, vector, full-text search)
    telemetry/         Session tracing, metrics events
    testutil/          Mock API server, test helpers
    tools/             50+ tool specs, registry, dispatch, handlers
  pkg/ycode/           Public embedding API (NewAgent, Run, functional options)
  external/            Git submodules (VictoriaLogs, Jaeger, Perses)
  configs/             Config templates, proxy landing page
  skills/              Skill definitions (build, deploy, validate)
  docs/                Architecture and feature documentation
```

---

## Runtime Flow

### 1. Entry

`cmd/ycode/main.go` → cobra CLI → either interactive REPL or one-shot mode. Optional self-healing wrapper catches panics and token-limit errors, attempts auto-recovery.

### 2. Conversation Loop

`internal/runtime/conversation/runtime.go` drives the agent loop:

1. Assemble system prompt (static + dynamic sections)
2. Send to provider (Anthropic or OpenAI-compatible)
3. Stream response tokens to terminal
4. If response contains tool calls → dispatch via `ToolExecutor` → append results → loop
5. If no tool calls → conversation turn complete

Loop detection (soft/hard thresholds) prevents infinite tool-call cycles. Token-limit errors trigger compaction and retry through a 3-layer defense: prune → compact → emergency flush.

### 3. Tool Dispatch

`internal/tools/registry.go` maps tool names to handlers. Tools are either:
- **Always-available** (sent in every API request): bash, read_file, write_file, edit_file, glob_search, grep_search
- **Deferred** (discovered via ToolSearch on demand): all other tools

Per-tool middleware wraps each handler with permission enforcement, logging, and timing. The registry supports runtime registration — plugins and MCP servers add tools without recompilation.

### 4. Provider Layer

`internal/api/` abstracts LLM providers:

- `Provider` interface: `Send(ctx, request) → response stream`
- `anthropic.go`: Anthropic API with SSE streaming
- `openai_compat.go`: OpenAI-compatible endpoints (OpenAI, xAI, Ollama, etc.)
- `prompt_cache.go`: fingerprints prompts (model, system, tools, messages) for cache hit detection. Completion TTL: 30s, prompt TTL: 5min. Detects unexpected cache breaks (>2K token drop without prompt change).

---

## System Prompt

Assembled by `internal/runtime/prompt/builder.go` in sections with a static/dynamic boundary for provider-side cache optimization:

```
┌───────��� STATIC (cacheable) ────────┐
│ Role description                    │
│ Output style                        │
│ Tool usage rules                    │
│ Task guidelines                     │
│ Action safety rules                 │
├─── DYNAMIC BOUNDARY ───────────────┤
│ Environment (model, CWD, platform) │
│ Git context (branch, commits)      │
│ Instruction files (CLAUDE.md chain)│
│ Recalled memories                  │
│ Runtime config                     │
└─────────────────────────────────────┘
```

Instruction files are discovered by walking from CWD to the filesystem root. Per-file budget: 4,000 chars; total: 12,000 chars. Deduplicated by content hash.

---

## Memory System

Five layers, from most to least volatile:

```
Working Memory        Context window — all messages in current session
    │
Short-term Memory     JSONL session files (~/.local/share/ycode/sessions/)
    │                 Rotates at 256KB, keeps 3 rotated files
    │
Long-term Memory      Auto-compaction at 100K tokens
    │                 Preserves last 4 messages verbatim
    │                 Semantic summary: scope, tools, requests, pending work,
    │                 key files, timeline. Budget: 1,200 chars / 24 lines.
    │
Contextual Memory     Instruction files (CLAUDE.md ancestry from CWD to root)
    │
Persistent Memory     File-based (~/.ycode/projects/{hash}/memory/)
                      MEMORY.md index (<200 lines)
                      Individual files with YAML frontmatter
                      Types: user, feedback, project, reference
```

### Context Management

Three-layer defense against context overflow:

1. **Prune**: soft trim of old tool results, then hard clear
2. **Compact**: semantic summary of older messages, preserve recent
3. **Emergency flush**: minimal continuation with summary + last user message

Model-aware budgets scale thresholds to the model's context window size.

---

## Session Management

Sessions are persisted as JSONL files. Each message is a JSON line with role, content blocks, and metadata. Sessions support:

- Resume via session ID or `latest`
- Auto-compaction producing semantic summaries
- Dual-write to SQLite for cross-session search and indexing
- Background Bleve indexing for full-text search across sessions

---

## Permission System

Three modes with increasing access:

| Mode | Scope |
|------|-------|
| ReadOnly | Read files, search, web fetch |
| WorkspaceWrite | Modify files within workspace boundaries |
| DangerFullAccess | Shell execution, process control, MCP |

Each tool declares its required permission level. The enforcer checks policy rules and prompts the user when needed. The VFS layer (`internal/runtime/vfs/`) validates every path against allowed directories, resolving symlinks and preventing escape.

---

## Observability Stack

A fully embedded observability stack runs as goroutines within a single binary — no external processes or downloads.

```
  ycode client (OTEL SDK)
       │ gRPC OTLP (:4317)
       ▼
  ┌────────────────────────────────────┐
  │  Embedded Server (:58080)          │
  │                                    │
  │  OTEL Collector (in-process)       │
  │    │ metrics    │ logs    │ traces │
  │    ▼            ▼         ▼        │
  │  Prometheus  VictoriaLogs  Jaeger  │
  │  (TSDB)     (submodule)   (submod) │
  │    │                               │
  │  Alertmanager   Perses Dashboards  │
  │                                    │
  │  Reverse Proxy + /healthz          │
  └────────────────────────────────────┘
```

| Signal | Pipeline |
|--------|----------|
| Metrics | OTEL SDK → Collector → Prometheus TSDB |
| Logs | OTEL SDK → Collector → VictoriaLogs |
| Traces | OTEL SDK → Collector → Jaeger |

Each ycode process gets a unique instance ID for per-client filtering. Components receive their reverse-proxy path prefix before starting so backends handle prefixed paths natively.

Auto-start behavior: if `ycode` launches and no server is running at the configured port, it starts the stack in background goroutines and shuts it down on exit.

---

## Storage Layer

`internal/storage/` provides a phased initialization pipeline:

| Phase | Backend | Latency | Purpose |
|-------|---------|---------|---------|
| 1 | KV (bbolt) | Instant | Config cache, permission history, indexer state |
| 2 | SQLite | ~50ms | Session index, tool metrics, dual-write |
| 3 | Vector | ~200ms | Code embeddings, semantic memory search |
| 3 | Search (Bleve) | ~200ms | Full-text codebase search, tool search index |

Phase 1 is synchronous at startup. Phases 2-3 initialize in background goroutines. Consumers wait on typed accessors (`SQL(ctx)`, `Vector(ctx)`) that block until the backend is ready or the context expires.

---

## Agent Patterns

### Continuous Loop

`ycode loop --interval 5m --prompt prompt.md` or `/loop 5m /review` in REPL. Reads the prompt file each iteration (edits take effect on next run), executes the agent, waits, repeats. Context carries over between iterations via session continuation.

### Recursive Delegation

Agents can spawn child agents up to a configurable depth (default: 3). Each agent type gets a tailored tool allowlist:

| Type | Tools |
|------|-------|
| Explore | Read-only (files, search, web) |
| Plan | Explore + task management |
| Verification | Plan + write + execute |
| General-purpose | All common tools |

### Markdown Working Memory

`.agents/ycode/scratchpad/` for per-session scratch files, `.agents/ycode/checkpoints/` for progress snapshots, `.agents/ycode/worklog.md` for append-only narrative. Auto-checkpoint on compaction when enabled.

---

## Skills System

Skills are directories containing instruction files and optional scripts:

```
skills/{name}/
  skill.md          Instructions with YAML frontmatter (name, description, triggers)
  scripts/          Optional executable scripts
  resources/        Data files, templates
```

Discovery chain: project `skills/` → `.agents/ycode/skills/` ancestors → `~/.ycode/skills/` → `$YCODE_SKILLS_DIR`.

---

## Plugin System

Plugins extend ycode with lifecycle hooks:

- **PreToolUse**: intercept/modify tool calls before execution
- **PostToolUse**: process results after execution
- **PostToolUseFailure**: handle errors

Plugins are discovered from `~/.ycode/plugins/` and project `.agents/ycode/plugins/`. Each plugin has a manifest declaring hooks, permissions, and dependencies.

---

## Configuration

Three-tier merge (later overrides earlier):

1. `~/.config/ycode/settings.json` (user)
2. `.agents/ycode/settings.json` (project)
3. `.agents/ycode/settings.local.json` (local, git-ignored)

Key settings: model, maxTokens, permissionMode, autoMemoryEnabled, autoCompactEnabled, autoDreamEnabled, fileCheckpointingEnabled, observability.

Config is cached in bbolt for cross-process access with stale detection.

---

## Self-Healing

`internal/selfheal/` wraps `main()` to catch panics and classified errors:

1. Classify error (token limit, build failure, API error, etc.)
2. If healable: attempt fix (rebuild, retry with backoff, compact context)
3. If AI provider available: ask a small model for diagnosis
4. Configurable: max attempts, protected paths, escalation policy

Disabled via `YCODE_SELF_HEAL=0`.

---

## Key Design Decisions

- **Map-based ToolRegistry** with runtime registration — plugins and MCP add tools without recompilation
- **`RuntimeContext` struct** holds all registries — no global state
- **`context.Context` propagation** everywhere for cancellation and timeout
- **JSONL sessions** for simplicity and interoperability
- **Section-based prompt assembly** with static/dynamic boundary for cache optimization
- **Per-tool middleware** for permission, logging, timing as composable wrappers
- **Embeddable library** via `pkg/ycode/` with functional options
- **Single binary** with all observability components as goroutines
- **Permissive licenses only** (MIT, Apache-2.0, BSD)

---

## Dependencies

| Purpose | Library | License |
|---------|---------|---------|
| CLI | `github.com/spf13/cobra` | Apache-2.0 |
| TUI | `github.com/charmbracelet/bubbletea` | MIT |
| Markdown | `github.com/charmbracelet/glamour` | MIT |
| Syntax | `github.com/alecthomas/chroma/v2` | MIT |
| UUID | `github.com/google/uuid` | BSD-3 |
| MCP | `github.com/modelcontextprotocol/go-sdk` | MIT |
| OTEL | `go.opentelemetry.io/otel/*` | Apache-2.0 |
| OTEL Collector | `go.opentelemetry.io/collector/*` | Apache-2.0 |
| Prometheus | `github.com/prometheus/prometheus` | Apache-2.0 |
| VictoriaLogs | `external/victorialogs` (submodule) | Apache-2.0 |
| Jaeger | `external/jaeger` (submodule) | Apache-2.0 |
| Perses | `external/perses` (submodule) | Apache-2.0 |
| Everything else | Go stdlib | BSD |

# ycode Architecture

ycode is a pure Go CLI agent harness for autonomous software development. It provides 50+ tools, multi-agent orchestration, MCP/LSP integration, a plugin system, permission enforcement, multi-layered memory, containerized browser automation, local LLM inference, and full observability. Single static binary, no external process dependencies.

---

## Project Layout

```
ycode/
  cmd/ycode/           CLI entry point (cobra commands, serve mode, OTEL wiring)
  internal/
    api/               Provider abstraction (Anthropic, OpenAI-compat), SSE streaming,
                         prompt cache, completion cache, cache warmer, fallback chains,
                         error classification, retry with backoff, request compression
    bus/               Event bus (local + NATS bridging)
    chat/              Chat hub, multi-channel messaging bridges
    cli/               Interactive REPL (bubbletea), markdown rendering (glamour + chroma)
    client/            Client library for remote connections
    collector/         Embedded OpenTelemetry Collector (in-process)
    commands/          Slash commands (/help, /config, /review, /skills, ...)
    container/         Podman container management — engine, images, networks,
                         container pool, machine provisioning, cleanup
    eval/              Evaluation harness (contract, behavioral, benchmark, smoke, e2e)
    gitserver/         Embedded Gitea git server for workspace isolation
    httputil/          HTTP utilities shared across server components
    inference/         Local LLM inference (Ollama runner, crash recovery, management UI)
    memos/             Long-term persistent memory via embedded Memos
    mesh/              Service mesh / multi-node coordination
    observability/     Embedded observability stack (Prometheus, Jaeger, VictoriaLogs, Perses)
    plugins/           Plugin manager, lifecycle hooks, discovery
    server/            HTTP/WebSocket API server + embedded NATS
    service/           Service layer for conversations and agents
    selfheal/          Self-healing wrapper (error classification, auto-rebuild)
    storage/           Persistence layer (KV/bbolt, SQLite, vector, Bleve full-text search)
    telemetry/         Session tracing, metrics events
    testutil/          Mock API server, test helpers
    tools/             50+ tool specs, registry, dispatch, handlers, quality monitoring
    web/               Embedded web UI (static assets)
    runtime/
      agentdef/        YAML-based agent definitions with inheritance, AOP advices, DAG flows
      agentpool/       Active subagent tracking with metrics
      astgrep/         Containerized ast-grep structural code search
      bash/            Shell execution with timeout, background jobs, safety classification
        shellparse/    AST-based shell command parser
      batch/           Bulk LLM prompt execution with checkpointing
      browseruse/      Containerized browser automation (Chromium + Playwright)
      builtin/         Single-shot optimized LLM operations (commit, init)
      config/          3-tier config merge (user > project > local)
      containertool/   Standardized tool containerization wrapper
      conversation/    Turn loop, tool dispatch, pre-activation, loop detection, recovery
      embedding/       Vector embeddings for semantic search
      fileops/         read, write, edit, glob, grep with VFS boundary enforcement
      git/             Git context discovery, worktrees, branch lock, stale detection
      health/          Periodic heartbeat diagnostics
      hooks/           Pre/post tool hooks
      indexer/         Background codebase indexing
      lanes/           Execution lane scheduling (main, cron, subagent)
      loop/            Continuous agent loop with cron scheduling
      lsp/             Language Server Protocol client
      mcp/             Model Context Protocol client/server, stdio/SSE transports
      memory/          Multi-layered memory (7 types, composite scoring, vector + Bleve search)
      net/             Web fetch, web search
      oauth/           PKCE OAuth flow
      permission/      Mode enforcement (ReadOnly, WorkspaceWrite, DangerFullAccess)
      policy/          Permission policy rules
      prompt/          System prompt assembly (section-based, cache-optimized, differential)
      ralph/           Autonomous iterative agent loop (step → check → commit → repeat)
      recovery/        Error recovery recipes
      repomap/         Token-budgeted repo overview with PageRank-scored symbols
        treesitter/    Multi-language symbol extraction via tree-sitter
      routing/         Multi-factor model/provider selection router
      scratchpad/      Markdown working memory, checkpoints, work log
      searxng/         Containerized SearXNG meta-search engine
      security/        Prompt injection detection (phrases, Unicode, base64)
      session/         JSONL persistence, compaction, LLM summarization, budgets
      swarm/           Multi-agent orchestration (hierarchical decomposition, handoffs)
      task/            Task registry
      taskqueue/       Bounded parallel execution with per-category semaphores
      team/            Team + cron registries
      todo/            Todo/checklist management
      usage/           Token counting, per-model cost estimation
      vfs/             Virtual filesystem with allowed-directory boundaries
      worker/          Worker boot lifecycle, trust gates
  pkg/ycode/           Public embedding API (NewAgent, Run, functional options)
  external/            Git submodules (VictoriaLogs, Jaeger, Perses, Memos)
  configs/             Config templates, proxy landing page
  skills/              Skill definitions (build, deploy, validate, learn, setup, claude)
  docs/                Feature documentation, research, roadmap
  e2e/                 Playwright browser tests
```

---

## Runtime Flow

### 1. Entry

`cmd/ycode/main.go` -> cobra CLI -> either interactive REPL or one-shot mode. Optional self-healing wrapper catches panics and token-limit errors, attempts auto-recovery. Container engine, SearXNG, and browser service initialized if container sandbox is enabled.

### 2. Conversation Loop

`internal/runtime/conversation/runtime.go` drives the agent loop:

1. **JIT discovery** — detect AGENTS.md/CLAUDE.md in accessed file paths
2. **L1 memory** — extract active topic from user message via TopicTracker
3. **Assemble system prompt** — static + dynamic sections with cache boundary
4. **Pre-activate tools** — three-tier pipeline (keyword -> scoring -> LLM classification)
5. **Build tool list** — always-available + activated deferred tools
6. **Send to provider** — stream response tokens to terminal
7. **If tool calls** — dispatch via bounded parallel executor -> append results -> loop
8. **If no tool calls** — conversation turn complete
9. **Context recovery** — auto-compaction if token budget exceeded

Loop detection (soft/hard thresholds) prevents infinite tool-call cycles. Token-limit errors trigger compaction and retry through a 3-layer defense: prune -> compact -> emergency flush.

### 3. Tool Dispatch

`internal/tools/registry.go` maps tool names to handlers. Tools are categorized by:

**Availability:**
- **Always-available** (sent in every API request): bash, read_file, write_file, edit_file, glob_search, grep_search, Skill, ToolSearch
- **Deferred** (discovered via ToolSearch on demand): all other tools, activated with TTL of 8 turns

**Concurrency category** (controls parallel execution limits):
- **Standard** (default 8): file ops, search, web
- **LLM** (default 2): tools that call LLM APIs
- **Agent** (default 4): subagent spawning
- **Interactive** (serialized): user prompts

Per-tool middleware wraps each handler with permission enforcement, logging, timing, and quality monitoring. The registry supports runtime registration — plugins and MCP servers add tools without recompilation.

### 4. Provider Layer

`internal/api/` abstracts LLM providers behind a single interface:

```go
type Provider interface {
    Send(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error)
    Kind() ProviderKind
}
```

**Implementations:**

| Provider | Implementation | Features |
|----------|---------------|----------|
| Anthropic | `anthropic.go` | Native API, SSE streaming, prompt caching with cache marks, gzip compression |
| OpenAI-compatible | `openai_compat.go` | Covers OpenAI, xAI/Grok, DashScope/Qwen, Moonshot/Kimi, Gemini, Ollama |

**Auto-detection** (`DetectProvider`): resolves provider from model name prefixes, explicit base URL, available API keys, or OAuth tokens. Two-tier alias resolution: user config aliases -> built-in aliases (e.g., `opus` -> `claude-opus-4-6-20250415`).

**Cross-provider tool marshaling:** unified `ContentBlock` format transparently converts between Anthropic's `tool_use`/`tool_result` and OpenAI's `tool_calls`/`tool` role messages, including streaming reconstruction of incremental JSON arguments.

**Reliability:**
- `FallbackProvider` — chains multiple providers, falls back on transient errors with cooldowns
- `retry.go` — exponential backoff with jitter (1s-128s), max 8 retries, retryable HTTP codes
- `errors.go` — classifies errors into recovery actions: retry, rotate key, fallback model, compress context, abort
- `compression.go` — gzip request bodies above 4KB threshold

---

## System Prompt

Assembled by `internal/runtime/prompt/builder.go` in sections with a static/dynamic boundary for provider-side cache optimization:

```
+--------- STATIC (cacheable) ----------+
| Role description                       |
| Personality (optional SOUL.md)         |
| Core system instructions               |
| Task guidelines                        |
| Action safety rules                    |
| Built-in skill optimizations           |
+--- DYNAMIC BOUNDARY ------------------+
| Filesystem access rules                |
| Environment (model, CWD, platform)     |
| Git context (branch, commits, diffs)   |
| Instruction files (CLAUDE.md ancestry) |
| Repository map (PageRank-scored)       |
| Recalled memories                      |
| Active topic (L1 working memory)       |
| Diagnostics (degraded tools, alerts)   |
+----------------------------------------+
```

**JIT instruction discovery** (`jit.go`): dynamically finds CLAUDE.md/AGENTS.md in directories accessed by tools. Walks directory tree, deduplicates by content hash, respects budget limits (5,000 chars/file, 10,000 total).

**Differential context injection** (`baseline.go`): for non-caching providers (OpenAI, etc.), tracks per-section SHA256 hashes. After the first turn, unchanged sections are replaced with `[System instructions unchanged...]` markers, saving ~1,500+ tokens/turn.

**Topic tracking** (`topic.go`): extracts current task focus from user messages (max 120 chars), auto-clears after 20 turns. Injected as lightweight `[Active Topic: ...]` signal.

---

## Memory System

Seven memory types across five storage tiers:

```
Working Memory        Context window — all messages in current session
    |
Short-term Memory     JSONL session files (~/.local/share/ycode/sessions/)
    |                 Rotates at 256KB, keeps 3 rotated files
    |
Long-term Memory      Auto-compaction at model-aware token thresholds
    |                 Preserves last 4 messages verbatim + first 2 user turns
    |                 LLM-based semantic summary (Haiku first, fallback to main model)
    |                 Budget: 1,200 chars / 24 lines
    |
Contextual Memory     Instruction files (CLAUDE.md ancestry from CWD to root)
    |                 JIT-discovered as tools access the filesystem
    |
Persistent Memory     File-based (~/.agents/ycode/projects/{hash}/memory/)
                      MEMORY.md index (<200 lines)
                      Individual files with YAML frontmatter
```

### Memory Types

| Type | Purpose |
|------|---------|
| User | User role, preferences, knowledge |
| Feedback | Guidance on approach — corrections and confirmations |
| Project | Ongoing work, goals, decisions |
| Reference | Pointers to external systems and resources |
| Episodic | Timestamped agent experiences — automatically created on subagent completion (tools used, duration, success/failure, session ID) |
| Procedural | Workflow patterns and decision-making heuristics |
| Task | Persistent structured task state |

### Memory Search

Multi-backend search with composite scoring (all backends wired at startup):

1. **Vector search** — semantic similarity via pluggable vector store (wired in vector store goroutine)
2. **Bleve full-text search** — keyword matching (wired in search store goroutine, existing memories indexed at startup)
3. **Fallback keyword search** — substring matching (name > description > content)

**Composite score:** `(Semantic * 0.5) + (Recency * 0.3) + (Importance * 0.2)` with exponential decay (30-day half-life) and project-scope boost (1.1x).

### Context Management

Three-layer defense against context overflow with model-aware budgets:

| Context Window | Reserved | Compaction Threshold |
|---------------|----------|---------------------|
| 32K | 8K | 20K |
| 64K | 16K | 40K |
| 128K | 30K | 80K |
| 200K | 40K | 100K |
| >200K | 20% | 50% |

Non-caching providers get 70% discount (compact earlier since every token costs).

**Dual trigger:** ratio-based OR reserved-buffer exhaustion (whichever fires first).

**Compaction algorithm:**
1. Protect first 2 user turns (context-setting) and last 4 messages
2. Reserve 8,000 tokens for recent messages
3. Summarize middle section via LLM (30s timeout, 1,024 token cap)
4. Extract and preserve task state
5. Enforce summary budget; fall back to heuristic if LLM fails
6. Prepend continuation preamble with summary for seamless handoff

---

## Prompt Cache Optimization

Three complementary caching mechanisms:

| Mechanism | Location | Purpose |
|-----------|----------|---------|
| Provider prompt cache | `prompt_cache.go` | Fingerprints request components (model, system, tools, messages) to detect cache hits/breaks. Wired into `Runtime.Turn()` — checks before each API call, updates after, detects breaks via token count drops. TTL: 5min. |
| Completion cache | `completion_cache.go` | Short-TTL response cache for identical requests. Returns cached response without API call on retries/recovery. |
| Cache warmer | `cache_warmer.go` | Background ping loop every 4.5min to keep Anthropic prompt cache alive. Sends minimal `max_tokens=1` request with same model/system/tools. |

---

## Tool System

### Tool Catalog (50+ tools)

38 handler registration functions install tools organized by domain:

**Core (always-available):**
- `bash` — shell execution with session state, background jobs, safety classification
- `read_file` / `write_file` / `edit_file` — VFS-validated file operations
- `glob_search` / `grep_search` — pattern matching with Bleve fallback
- `Skill` — invoke skill definitions
- `ToolSearch` — discover deferred tools by query

**Code Intelligence (deferred):**
- `symbol_search` — code symbol lookup (func/type/interface) by kind/language
- `semantic_search` — vector-based code search
- `ast_search` — containerized ast-grep structural pattern matching
- `references` — call chain tracing via RefGraph
- `LSP` — language server actions (definition, references, hover, symbols)

**File Operations (deferred):**
- `copy_file` / `move_file` / `delete_file` / `create_directory`
- `list_directory` / `tree` / `get_file_info` / `read_multiple_files`
- `list_roots` — allowed filesystem boundaries
- `apply_patch` — unified diff application
- `view_image` — image file viewing

**Web and Research (deferred):**
- `WebFetch` — URL fetching with readability-based content extraction
- `WebSearch` — multi-provider search with fallback chain
- `browser_navigate` / `browser_click` / `browser_type` / `browser_scroll` / `browser_screenshot` / `browser_extract` / `browser_back` / `browser_tabs` — containerized browser automation

**Git (deferred):**
- `git_status` / `git_log` / `git_commit` / `git_branch` / `git_stash`
- `view_diff` — staged/unstaged changes or commit ranges

**Memory and Persistence (deferred):**
- `memory_save` / `memory_recall` / `memory_forget` — cross-session memory
- `MemosStore` / `MemosSearch` / `MemosList` / `MemosDelete` — long-term storage

**Agent and Task Management (deferred):**
- `Agent` — spawn subagents with filtered tool access
- `Handoff` — agent-to-agent transfer signal
- `TaskCreate` / `TaskGet` / `TaskList` / `TaskUpdate` / `TaskStop` / `TaskOutput`

**Observability (deferred):**
- `query_metrics` / `query_traces` / `query_logs` — OTEL telemetry queries
- `run_tests` — test detection and execution

**Configuration and Mode (deferred):**
- `Config` — runtime configuration get/set
- `EnterPlanMode` / `ExitPlanMode` — plan mode transitions
- `compact_context` — manual context compaction
- `NotebookEdit` — Jupyter notebook editing
- `RemoteTrigger` — webhook invocation
- `Sleep` — delay execution
- `StructuredOutput` — JSON-structured agent responses

### Tool Discovery

ToolSearch implements a multi-tier discovery pipeline:

1. **Exact name match** (+12 points)
2. **Name contains term** (+8 points)
3. **Description contains term** (+4 points)
4. **Bleve full-text search** (semantic matching when index available)
5. **Bash native detection** — redirects CLI operations to bash tool

`select:Tool1,Tool2` syntax enables direct tool selection by name.

### Pre-Activation Pipeline

Before each turn, the runtime proactively activates likely-needed tools:

1. **Tier 1a** (< 0.1ms): high-precision keyword matching (git, deploy, test patterns)
2. **Tier 1b** (< 1ms): `SearchTools()` scoring with stop-word filtering
3. **Tier 2** (200-500ms, conditional): LLM classification via `InferenceRouter` — uses the main provider with `QualityMonitorStats` for multi-factor model selection

Activated tools stay available for 8 turns of non-use before expiring.

The `InferenceRouter` is wired in `cli/app.go` with the main provider as a classification candidate and the QualityMonitor as the stats provider.

### Quality Monitoring

`QualityMonitor` is initialized at startup (`main.go`) and attached to the registry. It tracks per-tool reliability:
- Total/success/failure counts, average duration
- Success rate with configurable threshold (default 0.7)
- Last failure timestamp for degradation detection
- Degraded tools surfaced in the system prompt diagnostics section
- Feeds into `InferenceRouter` via `QualityMonitorStats` for model routing decisions

---

## Bash Safety

`internal/runtime/bash/` implements defense-in-depth for shell execution:

**AST-based command parsing** (`shellparse/`): parses shell commands into AST nodes for structural analysis rather than string matching.

**Intent classification** (7 levels, 0-7 priority):

| Level | Intent | Examples |
|-------|--------|---------|
| 0 | ReadOnly | ls, cat, grep |
| 1 | Write | touch, mkdir, sed |
| 2 | Network | curl, wget, ssh |
| 3 | PackageManagement | apt, npm, pip |
| 4 | ProcessManagement | kill, ps, top |
| 5 | Destructive | rm -rf, dd |
| 6 | SystemAdmin | sudo, chown, chmod |
| 7 | Unknown | parse failure |

**Permission gating:** `ValidateForMode()` blocks operations that exceed the current permission mode. Plan mode (ReadOnly) prevents all write operations.

**Session state:** `ShellSession` tracks working directory across invocations, wrapping commands with `cd` and parsing the final cwd from output.

**Job management:** `JobRegistry` tracks background processes with ring-buffer output, supporting status queries and signal control (SIGINT/SIGTERM/SIGKILL).

**TTY support:** `TTYExecutor` interface provides pseudo-terminal access for interactive commands (ssh, sudo, password prompts).

---

## Agent Orchestration

### Agent Types

| Type | Mode | Tool Access | Use Case |
|------|------|-------------|----------|
| Explore | Read-only | bash, read_file, glob, grep, WebFetch, WebSearch, ToolSearch | Fast codebase exploration |
| Plan | Read-only | Explore + Skill, Agent, AskUserQuestion, plan mode tools | Architecture planning |
| Verification | Write | Explore + write_file, edit_file, bash, task management | Testing and validation |
| General-purpose | Build | All common tools | Multi-step implementation |
| Guide | Read-only | Explore + SendUserMessage | Documentation assistance |
| StatusLine | Limited | read_file, edit_file, Config | Configuration-only |

### Subagent Spawning

`NewAgentSpawner()` creates bounded agentic loops (max 15 turns per subagent):

1. `AgentManifest` specifies type, prompt, optional model override, background flag
2. Spawner creates `FilteredRegistry` with type-appropriate allowlist
3. `DefaultSubagentBlocklist` prevents recursion (Agent, Handoff, AskUserQuestion, memory writes, cron/trigger side effects)
4. `AgentPool` tracks active subagents with metrics (tool uses, tokens, status)

### Multi-Agent Orchestration (Swarm)

`internal/runtime/swarm/` provides higher-level coordination, wired through the agent spawner wrapper in `cli/app.go`:

- **Orchestrator** — manages handoff chains (agent-to-agent transfers with context variables). Automatically activated when a subagent returns a `__handoff__` JSON signal — the wrappedSpawner in `cli/app.go` detects handoff results and creates an Orchestrator to route to the target agent.
- **HierarchicalManager** — decomposes tasks into subtasks, delegates to specialist agents
- **Router** — routes work based on agent skill/role
- **Mailbox** — message passing between agents
- **SharedMemory** — shared context across agent instances
- **Cycle detection** — prevents infinite handoff loops with configurable max chain length (default 10)

### Agent Definitions

`internal/runtime/agentdef/` supports YAML-based agent configuration:

- Inheritance from parent definitions
- AOP advices (before/around/after hooks)
- Flow composition: Sequence, Chain, Parallel, Loop, Fallback, Choice, DAG
- `DAGExecutor` — topological workflow runner with layer-level concurrency
- Trigger patterns for keyword-activated agents

### Autonomous Loop (Ralph)

`internal/runtime/ralph/` implements a tight iterative loop for autonomous work:

1. **Step** — execute agent work function
2. **Check** — evaluate result (score-based, stagnation detection)
3. **Commit** — persist changes (git)
4. **Repeat** — until target score reached or max iterations

Supports automatic checkpointing, eval-driven termination, and optional fresh context spawning.

### Execution Lanes

`internal/runtime/lanes/` serializes execution across independent concerns, wired into the subagent spawner via `SpawnerConfig.LaneScheduler`:

- **Main** lane — serialized conversation work
- **Cron** lane — serialized scheduled tasks
- **Subagent** lane — bounded concurrent pool (default 4 slots)

Each subagent acquires a lane slot before executing. Prevents concurrency conflicts between main conversation and background work. The scheduler is initialized in `cli/app.go` and passed to the spawner config.

---

## Browser Automation

`internal/runtime/browseruse/` provides containerized browser automation:

**Container setup:**
- Image: `ycode-browser:latest` (Python 3.12-slim + Chromium + Playwright)
- Resources: 2 CPUs, 4GB memory
- Communication: JSON bridge via `container.Exec()`
- Lifecycle: lazy start on first browser tool call

**Actions:** navigate, click, type, scroll, screenshot (base64 PNG), extract (with NLP goal), back, tab management.

**Element handling:** up to 50 interactive elements per page with indexed references for LLM use. Page text truncated to 16KB.

**Fallback:** when browser service unavailable, tools return guidance to use `WebFetch` instead.

---

## Web Search

`internal/tools/web_search.go` implements a polymorphic search provider with automatic fallback:

| Priority | Provider | Activation |
|----------|----------|-----------|
| 1 | Containerized SearXNG | `cfg.Container.Enabled` or `YCODE_SEARXNG=true` |
| 2 | Brave Search API | `BRAVE_SEARCH_API_KEY` set |
| 3 | Tavily API | `TAVILY_API_KEY` set |
| 4 | Remote SearXNG | `SEARXNG_URL` set |
| 5 | DuckDuckGo | Always available (HTML scraping) |

**Containerized SearXNG** (`internal/runtime/searxng/`): official `searxng/searxng` image, health-checked on `/healthz`, queries via JSON API. Auto-started when container engine is enabled.

**WebFetch** (`internal/tools/web.go`): URL fetching with readability-based content extraction (`web_extract.go`), converting HTML to clean markdown for LLM consumption.

---

## Container Management

`internal/container/` provides full Podman lifecycle management via Go API bindings — no external `podman` binary calls.

### Engine

- **Linux/FreeBSD:** in-process REST API server (no VM)
- **macOS:** auto-provisions Linux VM via embedded vfkit hypervisor helper
- **Windows:** auto-provisions via Hyper-V
- Socket auto-discovery or explicit `CONTAINER_HOST` env var

### Capabilities

| Operation | Method |
|-----------|--------|
| Container lifecycle | Create, Start, Stop, Remove, Inspect |
| Command execution | Exec with stdout/stderr capture + exit code |
| File transfer | Bi-directional tar streaming (CopyTo/CopyFrom) |
| Image management | Pull, Build (Buildah), ImageExists, EnsureImage |
| Network isolation | Per-session bridge networks (`ycode-{sessionID}`) |
| Warm pool | Pre-warmed containers for sub-second agent startup |
| Cleanup | Orphan detection by session labels |

### Security Defaults

- All capabilities dropped (`CapDrop: ["ALL"]`)
- Optional read-only root filesystem
- Per-container CPU/memory limits
- Bind mounts, tmpfs, and port mappings configurable

### Integration Points

Container engine powers:
- Browser automation (Chromium + Playwright)
- SearXNG meta-search
- AST-grep structural code search
- Repo map tree-sitter parsing (non-Go languages)
- Agent sandbox isolation
- `containertool/` wrapper for standardized tool containerization

---

## Local LLM Inference (Ollama)

`internal/inference/` manages a local Ollama runner:

### Runner Lifecycle

**Discovery priority:**
1. Explicit `runnerPath` from config
2. `$OLLAMA_RUNNERS` env var
3. Embedded runner (self-extracting via `runner_embed/`)
4. Adjacent to ycode binary
5. System PATH

**Health checking:** HTTP GET with 30 retries, exponential backoff.

**Crash recovery:** monitors process exit, auto-restarts with exponential backoff (1s, 2s, 4s), max 3 restarts. Callbacks (`OnCrash`, `OnRestart`) emit OTEL traces.

### Integration

- `OllamaComponent` integrates with observability stack manager
- HTTP handler: reverse-proxy to runner at `/api/*` + embedded management SPA
- Model discovery: `DiscoverModels()` aggregates Ollama local models alongside cloud providers
- Provider routing: `internal/runtime/routing/` scores candidates (cost/latency/quality) to route cheap tasks to local models

---

## Model Routing

`internal/runtime/routing/` selects optimal model/provider per task:

- **Multi-factor scoring:** cost, latency, quality, resource availability
- **Task classification:** classification, embedding, summarization, commit message, general
- **Dynamic routing:** consumes OTEL telemetry and system load metrics
- **Single-shot optimization:** `internal/runtime/builtin/` bypasses the full conversation loop for quick operations (commit messages, init), using `ModelChain` (cheap -> capable fallback) for 90-95% token savings

---

## Permission System

Three modes with increasing access:

| Mode | Scope |
|------|-------|
| ReadOnly | Read files, search, web fetch |
| WorkspaceWrite | Modify files within workspace boundaries |
| DangerFullAccess | Shell execution, process control, MCP |

Each tool declares its required permission level in `ToolSpec.RequiredMode`. The enforcement pipeline in `Registry.Invoke()` evaluates in order:

1. **Policy engine** (`internal/runtime/policy/`) — rule-based decisions (allow/deny/ask) with priority ordering, tool name patterns, and path patterns. Highest priority — can override mode-based checks.
2. **Permission mode** — checks current mode against tool's required mode
3. **User prompter** — asks the user when mode doesn't allow (interactive mode) or returns clear error (one-shot mode)

The VFS layer (`internal/runtime/vfs/`) validates every path against allowed directories, resolving symlinks and preventing escape.

### Subagent Permission Inheritance

- Parent mode constrains subagent capabilities
- Plan-mode parent forces subagents to Explore type
- `FilteredRegistry` wraps base registry with per-agent allowlists
- Blocklists prevent dangerous tool access (Agent recursion, memory corruption, side effects)

---

## Security

### Prompt Injection Detection

`internal/runtime/security/` scans content for:
- Known injection phrases
- Invisible Unicode characters
- HTML tag injection
- Base64-encoded obfuscation

Findings rated Low/Medium/High severity, flagged before passing to LLM.

### Bash Safety

AST-based command parsing classifies intent (7 levels) and blocks operations exceeding current permission mode. See [Bash Safety](#bash-safety) section.

### VFS Boundaries

All file operations validated against allowed directory roots with symlink resolution and traversal prevention.

---

## Session Management

Sessions are persisted as JSONL files. Each message is a JSON line with UUID, role, content blocks, timestamp, model, and token usage. Sessions support:

- Resume via session ID or `latest`
- Auto-compaction producing semantic summaries
- Dual-write to SQLite for cross-session search and indexing
- Background Bleve indexing for full-text search across sessions
- Ghost snapshots for session resume with topic/summary preservation

---

## Observability Stack

A fully embedded observability stack runs as goroutines within a single binary:

```
  ycode client (OTEL SDK)
       | gRPC OTLP (:4317)
       v
  +------------------------------------+
  |  Embedded Server (:58080)          |
  |                                    |
  |  OTEL Collector (in-process)       |
  |    | metrics    | logs    | traces |
  |    v            v         v        |
  |  Prometheus  VictoriaLogs  Jaeger  |
  |  (TSDB)     (submodule)   (submod) |
  |    |                               |
  |  Alertmanager   Perses Dashboards  |
  |                                    |
  |  Reverse Proxy + /healthz          |
  +------------------------------------+
```

| Signal | Pipeline |
|--------|----------|
| Metrics | OTEL SDK -> Collector -> Prometheus TSDB |
| Logs | OTEL SDK -> Collector -> VictoriaLogs |
| Traces | OTEL SDK -> Collector -> Jaeger |

Each ycode process gets a unique instance ID for per-client filtering. Auto-start: if no server is running at the configured port, the stack launches in background goroutines and shuts down on exit.

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

Agents spawn child agents up to configurable depth (default: 3). Each agent type gets a tailored tool allowlist — see [Agent Types](#agent-types). Max 15 turns per subagent.

### Batch Execution

`ycode batch run --input prompts.jsonl` runs bulk LLM prompts from JSONL files. `internal/runtime/batch/Runner.Run()` provides bounded parallelism (configurable concurrency), automatic checkpointing for resume on failure, per-prompt retry logic, and cost/token tracking. Results written to JSONL output.

### Ralph Autonomous Loop

`ycode ralph "implement feature X" --check "go test ./..." --commit` runs the Ralph loop:

1. **Step** — execute LLM call with iteration context and previous results
2. **Check** — run verification command (e.g., test suite)
3. **Commit** — auto-commit on success (optional)
4. **Repeat** — until target score reached, max iterations, or stagnation detected

Supports PRD-driven story mode, configurable timeout, and OTEL tracing per iteration.

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

Discovery chain: project `skills/` -> `.agents/ycode/skills/` ancestors -> `~/.ycode/skills/` -> `$YCODE_SKILLS_DIR`.

**Skill evolution** (`internal/tools/evolution.go`): LLM-backed analysis of execution traces proposes skill improvements, derived variants, or new captures based on tool failure patterns.

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

Key settings: model, maxTokens, permissionMode, autoMemoryEnabled, autoCompactEnabled, autoDreamEnabled, fileCheckpointingEnabled, observability, container, parallel limits.

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

## Provider Capabilities

| Provider | Caching | Context Window | Notes |
|----------|---------|----------------|-------|
| Anthropic | Explicit (cache marks) | 200K tokens | 5-min TTL, cache warmer keeps alive |
| Gemini | Implicit prefix | 1M tokens | Automatic, not controllable |
| OpenAI | None (implicit prefix) | 128K-200K tokens | Differential injection compensates |
| Ollama (local) | None | Model-dependent | Fastest for small tasks |

Non-caching providers use differential context injection to reduce per-turn token cost.

---

## Token Counting and Cost Tracking

`internal/runtime/usage/` provides per-model pricing and thread-safe usage tracking:

| Model | Input/1M | Output/1M | Cache Write/1M | Cache Read/1M |
|-------|----------|-----------|----------------|---------------|
| Claude Opus 4 | $15 | $75 | $18.75 | $1.50 |
| Claude Sonnet 4 | $3 | $15 | $3.75 | $0.30 |
| Claude Haiku 4.5 | $0.80 | $4 | $1.00 | $0.08 |
| GPT-4o | $2.50 | $10 | — | $1.25 |
| Gemini Pro | $1.25 | $10 | — | $0.315 |

`Tracker` accumulates input/output/cache tokens per model and estimates USD cost.

---

## Key Design Decisions

- **Map-based ToolRegistry** with runtime registration — plugins and MCP add tools without recompilation
- **`RuntimeContext` struct** holds all registries — no global state
- **`context.Context` propagation** everywhere for cancellation and timeout
- **JSONL sessions** for simplicity and interoperability
- **Section-based prompt assembly** with static/dynamic boundary for cache optimization
- **Differential context injection** for non-caching providers
- **Per-tool middleware** for permission, logging, timing as composable wrappers
- **Bounded parallel tool execution** with per-category semaphores
- **Multi-backend memory search** with composite scoring (vector + full-text + keyword)
- **Containerized tools** (browser, search, AST) via Go API bindings — no external binary calls
- **Model routing** scores cost/latency/quality to select optimal provider per task
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
| Podman | `go.podman.io/podman/v6` | Apache-2.0 |
| Bleve | `github.com/blevesearch/bleve/v2` | Apache-2.0 |
| VictoriaLogs | `external/victorialogs` (submodule) | Apache-2.0 |
| Jaeger | `external/jaeger` (submodule) | Apache-2.0 |
| Perses | `external/perses` (submodule) | Apache-2.0 |
| Everything else | Go stdlib | BSD |

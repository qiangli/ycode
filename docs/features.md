# ycode Feature Reference

Comprehensive inventory of all ycode features, validated against source code.
Pure Go CLI agent harness for autonomous software development.

---

## 1. Slash Commands (Skills)

User-invocable skills discovered from `skills/*/skill.md`, `~/.agents/ycode/skills/`, and embedded builtins.

| Skill | Description | Source |
|-------|-------------|--------|
| `/init` | Initialize AGENTS.md with codebase documentation | Embedded |
| `/commit` | Create git commit with conventional message | Embedded |
| `/build` | Build ycode binary with full quality gate | Disk |
| `/setup` | Set up development environment and verify build | Disk |
| `/deploy` | Deploy ycode to localhost or remote host via SSH | Disk |
| `/validate` | Run integration, smoke, acceptance, and performance tests | Disk |
| `/eval` | Benchmark ycode vs other AI coding agents (aperio) | Disk |
| `/learn` | Study prior-art projects, produce gap analysis and implementation plan | Disk |
| `/autopilot` | Autonomous agentic tool analysis across three domains (A/M/T) | Disk |
| `/audit` | Security and compliance audits (dependencies, licenses, telemetry) | Disk |
| `/oci` | Build and run projects in OCI containers | Disk + Embedded |
| `/claude` | Run Claude Code CLI with a single prompt | Disk |
| `/bench-instructions` | Benchmark instruction file quality across tools | Disk |

**Discovery mechanism**: Builtin executors checked first (fast path), then disk search walks project → home → env directories. Skills are markdown documents with YAML frontmatter dispatched by the `/skill` tool handler.

**Packages**: `internal/tools/skill.go`, `internal/runtime/builtin/`, `skills/*/skill.md`

---

## 2. CLI Commands

Cobra-based CLI with subcommands for different operational modes.

| Command | Description |
|---------|-------------|
| `ycode` | Interactive REPL mode (default) |
| `ycode prompt <msg>` | One-shot agent prompt |
| `ycode serve` | Start server (Gitea, Ollama, SearXNG on :58080) |
| `ycode mcp serve` | Expose ycode tools via MCP server |
| `ycode version` | Print version info |
| `ycode doctor` | Health checks and readiness diagnostics |
| `ycode login / logout` | OAuth authentication |
| `ycode loop` | Continuous loop mode with interval |
| `ycode auto <goal>` | RESEARCH→PLAN→BUILD→EVALUATE→LEARN autonomous loop |
| `ycode sprint <goal>` | Structured sprint (milestones/slices/tasks) |
| `ycode ralph` | Autonomous learning loop |
| `ycode mesh` | Autonomous agent mesh control |
| `ycode heal` | Self-healing diagnostics (+ `status`, `test` subcommands) |
| `ycode model` | Manage local LLM models (Ollama/HuggingFace) |
| `ycode podman` | Container management (alias: `docker`) |
| `ycode train` | Training and evaluation (RL, data collection) |
| `ycode skill` | Skill engine (list, show, inspect) |
| `ycode eval` | Evaluation framework (aperio benchmarks) |
| `ycode batch` | Batch processing mode |

**Packages**: `cmd/ycode/main.go`, `cmd/ycode/*.go`, `internal/cli/app.go`

---

## 3. Agent Orchestration & Workflow Management

Multi-agent coordination, task delegation, autonomous operation.

### Core Conversation Loop
- Turn execution with streaming, tool dispatch, and 4-layer context defense (mask→prune→compact→flush)
- Prompt caching fingerprinting, completion caching, JIT instruction discovery
- Topic tracking and persona modeling per-turn
- **Package**: `internal/runtime/conversation/` (6,728 LOC, 14 test files)

### Swarm Orchestration
- Multi-agent handoff chains with cycle detection (max 10 hops)
- Context variable merging across agents
- Dynamic agent routing and mailbox-based async messaging
- **Package**: `internal/runtime/swarm/` (1,902 LOC, 6 test files)

### Mesh Background Agents
- 5-agent diagnostic mesh: Diagnoser, Fixer, Learner, Researcher, Trainer
- Event-bus driven (tool degradation, latency spikes, error rates, token waste detection)
- OTEL-traced with safety guards (max fix attempts, cooldown)
- **Package**: `internal/mesh/` (1,919 LOC, 6 test files)

### Agent Definitions (YAML DSL)
- Custom system prompts, model overrides, tool allowlists, inheritance
- Flow types: sequence, chain, parallel, loop, fallback, choice, DAG, router
- AOP advices (before/around/after), output schemas, guardrails
- Remote A2A delegation, conditional routing, quality gates
- **Package**: `internal/runtime/agentdef/` (4,265 LOC, 10 test files)

### Autonomous Loop
- RESEARCH→PLAN→BUILD→EVALUATE→LEARN cycle with pluggable callbacks
- Stall detection with dual-loop replanning (MagenticOne-inspired)
- Circuit breaker (closed/open/half-open) with cooldown
- Exit control with dual-layer detection (heuristic + explicit signal)
- Question suppression for headless autonomous mode
- Wakeup queue with coalescing and priority ordering
- **Package**: `internal/runtime/autoloop/` (1,646 LOC, 5 test files)

### Sprint Management
- Task execution with two-stage review (initial + refined)
- State machine (Plan→Execute→Complete), max retry attempts, token budget
- Slice-based dependencies, persistence to disk
- **Package**: `internal/runtime/sprint/` (1,632 LOC, 5 test files)

### Self-Healing
- Autonomous error diagnosis, fix, rebuild, restart
- Error classification: build, runtime, config, API, tool, inference
- State machine with configurable escalation policy (ask/escalate/ignore)
- AI-guided diagnosis via LLM, protected path enforcement
- **Package**: `internal/selfheal/` (2,179 LOC, 1 test file)

### Skill Engine
- Semantic skill selection with success/failure tracking
- Auto-evolution (FIX flag at <50% success after 3+ failures)
- Weekly decay to favor recent skills
- **Package**: `internal/runtime/skillengine/` (975 LOC, 2 test files)

### Hook System
- Events: PreToolUse, PostToolUse, PostToolFailure, SessionStart, SessionEnd, FileChanged, TurnStart
- Pattern matching on tool names (regex), priority ordering
- Input modification, block/continue actions with custom messages
- Shell commands with JSON protocol, user-configurable via settings.json
- **Package**: `internal/runtime/hooks/` (538 LOC, 1 test file)

### Agent Pool & Progress Reporting
- Per-agent metrics: status, tool use count, current tool, tokens, duration
- Liveness classification (healthy/suspicious/critical/stranded) with self-reported state
- Orphan recovery for stranded/critical subagents
- Tree-style progress display for concurrent agents
- **Package**: `internal/runtime/agentpool/` (854 LOC, 3 test files)

### Loop Detection
- 5 detectors: generic-repeat, unknown-tool, poll-no-progress, ping-pong, global-ceiling
- Progressive severity (warning → critical) with deterministic outcome hashing
- Configurable thresholds, guidance message injection
- **Package**: `internal/runtime/loopdetect/` (702 LOC, 1 test file)

### Task & Todo System
- Hierarchical task trees with parent-child relationships and async mailboxes
- Bounded parallel executor with per-category semaphore pools
- Todo board with dependencies, assignment, priority, JSON persistence
- **Packages**: `internal/runtime/task/` (640 LOC), `internal/runtime/taskqueue/` (646 LOC), `internal/runtime/todo/` (578 LOC)

---

## 4. Memory Management & Context Engineering

Five-layer memory system with multi-backend retrieval.

### Session Management
- JSONL-based message persistence with rotation (256KB files, 3 rotations)
- CJK-aware token estimation (0.25 ASCII, 1.3 CJK tokens/char)
- Observation masking: 3-layer pruning (soft trim → hard clear → mask)
- LLM-based compaction with head/tail protection
- Microcompaction for granular message-level reduction
- Child sessions for parallel workstreams
- Identifier preservation during compaction (file paths, git hashes, UUIDs)
- Transcript repair for orphaned tool_use/tool_result pairs
- Stuck detection and session diagnostics
- **Package**: `internal/runtime/session/` (5,892 LOC, 28 test files)

### Memory System
- **7 types**: User, Feedback, Project, Reference, Episodic, Procedural, Task
- **4 scopes**: Global, Project, Team, User (hierarchical paths)
- TTL-driven expiration with temporal validity windows (valid_from/valid_until)
- Content deduplication via MD5 hash matching
- Dynamic value scoring (importance + access + feedback)
- Entity extraction (NER) and entity-based linking
- Dreamer: background consolidation on 30-min intervals with LLM-backed merge
- Persona system: knowledge domains, communication style, behavior profiling
- **Package**: `pkg/memex/memory/` (5,356 LOC, 22 test files)

### Memory Retrieval
- **4 search backends**: keyword matching, Bleve full-text, vector semantic, entity-based
- Reciprocal Rank Fusion (RRF) across backends
- Maximal Marginal Relevance (MMR) diversity re-ranking
- Adaptive-depth recall with confidence-based deepening and LLM sub-queries
- **Package**: `pkg/memex/memory/` (integrated)

### Prompt Assembly
- **18 prompt sections**: intro, system, tasks, actions, environment, project, git, instructions, memory, config, filesystem, builtin-skills, active-topic, personality, platform, repo-map, persona, diagnostics
- Static/dynamic boundary for prompt caching (cacheable sections before boundary)
- Differential prompting: omits unchanged sections on non-caching providers
- JIT discovery of AGENTS.md/CLAUDE.md with hash-based dedup
- Prompt-injection detection for instruction files
- **Package**: `internal/runtime/prompt/` (1,985 LOC, extensive tests)

### API-Level Caching
- **PromptCache**: fingerprinting (model/system/tools/messages hashes), 5-min TTL, cache-break detection
- **CacheWarmer**: background keep-alive pings (4.5-min interval to beat Anthropic TTL)
- **CompletionCache**: full response caching with request hash, 30s TTL, disk-backed
- **Package**: `internal/api/` (3,108 LOC, 13 test files)

### Persistence Layer
- **4 backends**: KVStore (bbolt), SQLStore (modernc.org/sqlite), VectorStore (chromem-go), SearchIndex (Bleve)
- Progressive initialization: Phase 1 (KV instant), Phase 2 (SQL background), Phase 3 (search lazy)
- Pure Go, zero CGO, permissive licenses only
- **Package**: `pkg/memex/store/` (1,529 LOC, tests per backend)

---

## 5. Built-in Search

### Web Search
- **SearXNG meta-search**: containerized AGPL-isolated engine (HTTP-only boundary)
- **Multi-provider fallback**: SearXNG → Brave API → Tavily API → external SearXNG URL → DuckDuckGo
- **WebFetch**: HTML-to-markdown conversion with readability extraction
- Health-checked container lifecycle, 15s search timeout, configurable result limits
- **Packages**: `internal/runtime/searxng/` (364 LOC), `internal/tools/web_search.go`, `internal/tools/web.go`

### File & Codebase Search
- **Glob search**: `doublestar` recursive patterns, sorted by modification time, 100-result cap
- **Grep search**: regex with context lines (-B/-C/-A), 3 output modes (content/files/count), file type filtering
- **Indexed grep**: two-stage optimization (Bleve index → candidate files → regex verification), literal extraction from pattern AST
- **Symbol search**: Bleve-indexed symbol database filtered by kind/language/export status
- **AST search**: tree-sitter powered structural code search with pattern wildcards (`$VAR`, `$$$VAR`)
- **ToolSearch**: semantic deferred tool discovery via Bleve search index with TTL-based activation
- **Packages**: `internal/runtime/fileops/` (glob.go, grep.go, grep_indexed.go), `internal/tools/` (symbol_search.go, ast_search.go)

### Code Intelligence
- **Tree-sitter AST**: pure Go bindings (gotreesitter) for Go, Python, JS/TS, Rust, Java, C, Ruby
- **Repo map**: token-budgeted file→symbol overview with PageRank relevance scoring (Aider-inspired)
- **LSP integration**: JSON-RPC client for gopls, pyright, tsserver — hover, definitions, references, symbols, diagnostics
- **Background indexer**: scans workspace every 5 min, chunks files (4KB), indexes into Bleve + symbol index
- **Reference graph**: tracks cross-file Go dependencies for impact analysis
- **Packages**: `internal/runtime/treesitter/` (487 LOC), `internal/runtime/repomap/` (322 LOC), `internal/runtime/lsp/` (485 LOC)

### Full-Text & Semantic Search
- **Bleve FTS**: BM25 scoring, fuzzy matching, phrase queries, faceted metadata search
- **Vector store**: chromem-go embeddings with cosine/euclidean/dot-product similarity
- Dual indices: "code" (file chunks) + "symbols" (extracted definitions)
- **Packages**: `pkg/memex/store/search/` (330 LOC), `pkg/memex/store/vector/` (228 LOC)

---

## 6. Tool System & Function Calling

### Tool Registry
- 130+ registered tools across 10+ categories
- Toolset composition: core, web, git, research, full_stack, read_only profiles
- Deferred tool discovery via ToolSearch with TTL-based activation (8 turns)
- Tool middleware chain for retry, caching, logging, metrics
- Per-category semaphore pools (standard: 8, LLM: 2, agent: 4, interactive: 2)
- Quality monitoring: success/failure rate tracking per tool
- **Package**: `internal/tools/` (20,700+ LOC, 32 test files)

### Core Tools (always available)
`bash`, `read_file`, `write_file`, `edit_file`, `glob_search`, `grep_search`, `Skill`, `ToolSearch`, `Agent`

### File Operations
`copy_file`, `move_file`, `delete_file`, `create_directory`, `list_directory`, `tree`, `get_file_info`, `read_multiple_files`, `read_document`, `view_image`, `view_diff`, `apply_patch`

### Git Tools (deferred)
`git_status`, `git_log`, `git_commit`, `git_branch`, `git_stash`, `git_add`, `git_reset`, `git_show`, `git_grep`

### Memory Tools
`memory_save`, `memory_recall`, `memory_forget`, `memory_list`

### Task & Worker Tools
`TaskCreate`, `TaskGet`, `TaskList`, `TaskUpdate`, `TaskStop`, `TaskOutput`, `WorkerCreate`, `WorkerGet`, `WorkerObserve`, `WorkerSendPrompt`, `WorkerRestart`, `WorkerTerminate`

### Planning & Agent Tools
`UpdatePlan`, `ListPlan`, `SetGoal`, `GetGoal`, `SetTaskStatus`, `Think`, `EnterPlanMode`, `ExitPlanMode`, `ParallelAgents`, `AgentList`, `AgentWait`, `AgentClose`

### Observability Tools
`query_metrics`, `query_traces`, `query_logs`, `compact_context`

### MCP Tools
`MCP`, `ListMcpResources`, `ReadMcpResource`, `McpAuth`

### Misc
`AskUserQuestion`, `SendUserMessage`, `AttemptCompletion`, `CreateRule`, `Sleep`, `NotebookEdit`, `RemoteTrigger`, `Config`, `TodoWrite`, `run_tests`

---

## 7. Bash & Shell Support

### Execution Engine
- In-process command execution with 120s default timeout (max 10 min)
- 512 KB output limit with ringbuffer truncation
- Background job registry with signal handling (SIGTERM, SIGKILL)
- TTY detection for interactive commands (sudo, ssh, gcloud, passwd)
- Session isolation with per-session working directory tracking
- Output spill to disk for large outputs with auto-cleanup (7-day retention)
- **Package**: `internal/runtime/bash/` (4,300+ LOC, 14 test files)

### Safety & Security
- **CommandIntent classification** (8 levels): ReadOnly, Write, Destructive, Network, ProcessManagement, PackageManagement, SystemAdmin, Unknown
- AST-based shell parsing (`shellparse` subpackage) for accurate intent detection
- Validator integration with permission modes before execution
- Probe for command availability
- Output cleanup and shell artifact stripping
- **Package**: `internal/runtime/bash/safety.go` (20,500 LOC including tests)

### Security Scanning
- Prompt injection detection: known phrases, invisible Unicode, HTML tags, Base64 blocks
- Severity classification (Low, Medium, High) with finding snippets
- **Package**: `internal/runtime/security/` (5,500 LOC)

---

## 8. Browser Automation

- LLM-optimized browser automation via `browser-use` Python library in container
- Actions: navigate, click, type, scroll, tab management, screenshot, goal-driven tasks
- Long-lived stateful container across turns (multi-tab support)
- DOM extraction optimized for LLM consumption
- 30s per-action timeout, 10-min build timeout
- **Package**: `internal/runtime/browseruse/` (250 LOC)

---

## 9. Virtual Filesystem (VFS)

- Sandbox enforcement with configurable allowed directory boundaries
- Symlink resolution with escape detection and user prompting
- 10 MB file size limit
- Prefix-matching attack prevention (e.g., `/tmp/foo` vs `/tmp/foobar`)
- SSRF protection for network requests: blocks private CIDR ranges (loopback, RFC1918, link-local, CGNAT, cloud metadata)
- **Packages**: `internal/runtime/vfs/` (300 LOC, 2 test files), `internal/runtime/net/` (150 LOC, tests)

---

## 10. Internal OCI Container Support & Sandboxing

### Embedded Podman Engine
- Pure Go REST API bindings — no external `docker` or `podman` binary required
- Three-tier bootstrap: existing socket → in-process API server (Linux) → auto-provisioned VM (macOS/Windows)
- Container, Pod, Network, Image, Machine management
- OTEL instrumentation for container lifecycle events
- **Package**: `internal/container/` (2,500 LOC, 8 test files including integration)

### Container Tool Pattern
- Standardized abstraction for running tools inside containers
- Inline Dockerfile, source file embedding, bind mounts, build/run timeouts
- Mutex-protected image builds for concurrent safety
- Used by: browser-use, SearXNG, tree-sitter (ast-grep), custom tools
- **Package**: `internal/runtime/containertool/` (150 LOC)

### File Operations Security
- Atomic writes (temp file → rename) to prevent corruption
- Sensitive file detection (secrets, credentials, private keys)
- Binary file detection, device file blocking (/dev, /proc, /sys)
- .gitignore/.ignore respect
- Per-file edit locking (semaphore) for concurrent subagent safety
- Encoding detection with auto-conversion (UTF-8, BOM handling)
- 4-level fuzzy edit matching (exact → line-trimmed → indent-normalized → block-anchor)
- **Package**: `internal/runtime/fileops/` (3,600 LOC, 16 test files)

---

## 11. Internal LLM Inference Support

### Embedded Ollama Runner
- Manages Ollama C++ runner as subprocess — no external installation required
- Auto-discovers binary from config, env var, adjacent path, or PATH
- Health monitoring with auto-restart (max 3 retries)
- Ephemeral port allocation, stdout/stderr redirection
- Crash callbacks wired to OTEL telemetry
- HTTP reverse proxy for Ollama API (`/ollama/api/*`)
- Embedded management UI (`/ollama/`)
- **Package**: `internal/inference/` (1,200 LOC, 3 test files)

### Model Management
- HuggingFace provider discovery and download
- Custom models directory support
- Embedding runner for vector operations
- CLI: `ycode model` subcommand for pull/list/remove

### Provider Layer
- `Provider` interface: `Send(ctx, *Request) → stream`
- Anthropic native SDK + OpenAI-compatible (covers OpenAI, xAI, Gemini, Ollama)
- Model alias resolution with automatic fallback
- Retry logic with exponential backoff
- **Package**: `internal/api/` (3,108 LOC, 13 test files)

---

## 12. Built-in OpenTelemetry

### OTEL Provider
- **Dual export**: gRPC to OTEL collector + file persistence for offline review
- Trace, Metric, and Log providers with configurable exporters (gRPC, stdout, file)
- File persistence at `~/.agents/ycode/otel/instances/{id}/`
- Retention policies with automatic cleanup
- **Package**: `internal/telemetry/otel/` (1,800 LOC, tests)

### Instrumentation
- **SessionTracer**: in-memory trace records (api_call, tool_use, command, compaction, error)
- **AnalyticsCollector**: event aggregation (prompt, tool, command, error types) with token counting
- **ConversationLogger**: structured per-turn logging with redaction
- **RequestLogger**: API request capture with sensitive data redaction
- **DiagnosticBridge**: integrates mesh diagnostic reports into OTEL logs
- **CostTracker**: per-model token cost calculation
- Tools: `query_metrics`, `query_traces`, `query_logs` for agent self-observation

### Server Integration
- WebSocket hub with OTEL-instrumented connection lifecycle
- Bearer token auth, request logging, conversation logging
- **Packages**: `internal/telemetry/` (1,800 LOC), `internal/server/` (600 LOC)

---

## 13. Git Integration

### Native Go-Git Execution
- 3-tier fallback: Tier 1 (pure go-git) → Tier 2 (filtered subprocess) → Tier 3 (full subprocess with safety)
- 31 native functions: rev-parse, status, log, diff, blame, add, commit, branch, stash, reset, show, grep
- Author resolution from git config with default signature fallback
- No shell invocation for git operations — eliminates injection vectors
- **Package**: `internal/runtime/toolexec/` (9 files, 117,000+ LOC including tests)

### Embedded Git Server
- In-process Gitea instance (no external binary)
- Local repos for agent swarm coordination
- HTTP + MCP interfaces, workspace API for repo operations
- Tools: `GitServerRepoList`, `GitServerRepoCreate`, `GitServerWorktreeCreate`, `GitServerWorktreeMerge`, `GitServerWorktreeCleanup`
- **Package**: `internal/gitserver/` (550 LOC, integration tests)

### GitHub Integration
- PR create/list/review/comment, issue list/get/comment, CI check status
- Auth: `GITHUB_TOKEN` → `GH_TOKEN` → `~/.config/gh/hosts.yml` (no external `gh` binary)
- Tools registered as deferred (via ToolSearch)
- **Package**: `internal/runtime/github/` (250 LOC, tests)

---

## 14. MCP (Model Context Protocol)

- **Server mode**: `ycode mcp serve` exposes ycode tools via MCP JSON-RPC
- **Client**: connects to external MCP servers via stdio and SSE transports
- Config-driven: `~/.config/ycode/mcp.json` or `.agents/ycode/mcp.json`
- Tools: `MCP`, `ListMcpResources`, `ReadMcpResource`, `McpAuth`
- **Package**: `internal/runtime/mcp/` (400 LOC, tests)

---

## 15. Configuration & Permission System

### Four-Tier Config Merge
1. `~/.config/ycode/settings.json` (user)
2. `<project>/.agents/ycode/settings.json` (project)
3. `<cwd>/.agents/ycode/settings.json` (local)
4. `settings.local.json` (gitignored)

`Instructions` and `AllowedDirectories` append; all other fields override.

### Permission Modes
- **ReadOnly**: file reads and search only
- **WorkspaceWrite**: file modifications within VFS boundaries
- **DangerFullAccess**: shell, process control, MCP, external APIs

### Policy Engine
- Rule-based decisions (Allow, Deny, Ask) with tool name glob + optional args matching
- Async approval routing with timeout (5 min default)
- Decision caching for repeated tool calls
- **Package**: `internal/runtime/permission/` (7 files, tests)

---

## 16. Training & Evaluation

### Training Framework
- Reinforcement learning loop with reward computation
- Data collection and trajectory parsing
- Rollout execution with task evaluation
- **Packages**: `internal/training/` (loop, parsers, reward, rollout, task — all with tests)

### Evaluation Framework
- Contract-tier evals (no LLM, deterministic)
- Smoke-tier evals (real LLM, requires provider)
- Behavioral evals (trajectory analysis)
- E2E evals (full coding tasks)
- AGENTS.md quality validation
- **Package**: `internal/eval/`, Makefile targets: `eval-contract`, `eval-smoke`, `eval-behavioral`, `eval-e2e`

---

## Test Coverage Summary

| Category | Test Files | Test Functions | E2E/Integration |
|----------|-----------|---------------|-----------------|
| Unit tests | 375 | 2,607 | — |
| Integration tests (Go) | 15 | 196 | `internal/integration/` |
| E2E tests (Go) | 7 | 90 | CLI, TUI, git, tree-sitter, repomap |
| Playwright browser tests | 5 | — | `e2e/tests/` |
| TUI fuzz tests | — | — | `make test-tui-fuzz` |
| Container integration | 2 | 6 | `internal/container/` |
| **Total** | **404** | **2,899+** | **29 files** |

### Test Targets

```
make test              # unit tests (-short -race)
make test-container    # container integration (requires podman)
make test-gitserver    # git server workspace tests
make test-tui          # TUI integration (direct Update + teatest)
make test-tui-e2e      # TUI E2E in PTY (requires compiled binary)
make test-tui-fuzz     # TUI fuzz tests (30s each)
make test-ui           # Playwright browser tests
make test-all          # all combined
make validate          # Go integration tests against running server
make validate-ui       # Playwright against running server
make validate-all      # both
```

All 111 Go packages compile. 85 packages with tests pass (0 failures). 26 packages have no test files (mostly entry points, interfaces, and wire packages).

---

## Architecture Principles

- **No global state**: all state on `RuntimeContext` or function parameters
- **Pure Go, zero CGO**: permissive licenses only (MIT, Apache-2.0, BSD, ISC, MPL-2.0)
- **Embedded infrastructure**: Ollama, Podman, Gitea, SearXNG — no external binaries
- **Layered build system**: Makefile (dependency graph) → scripts/ (orchestration) → Go (logic)
- **Progressive initialization**: KV instant → SQL background → vector/search lazy
- **Four-tier config merge**: user → project → local → gitignored
- **Five-layer memory**: working → episodic → compaction → procedural → persistent

# ycode Architecture

ycode is a pure Go CLI agent harness for autonomous software development. It orchestrates LLM providers, tool systems, memory management, and agent swarms to provide a comprehensive coding assistant.

**Version:** 1.0+  
**Go Version:** 1.26+  
**License:** MIT (dependencies are Apache-2.0 or BSD)

---

## Table of Contents

1. [Overview](#overview)
2. [Repository Layout](#repository-layout)
3. [Core Runtime Flow](#core-runtime-flow)
4. [Memory & Context Management](#memory--context-management)
5. [Agent Orchestration](#agent-orchestration)
6. [Tool System](#tool-system)
7. [LLM Provider Layer](#llm-provider-layer)
8. [Configuration System](#configuration-system)
9. [Permission & Security Model](#permission--security-model)
10. [Container & Browser Infrastructure](#container--browser-infrastructure)
11. [Local LLM Integration (Ollama)](#local-llm-integration-ollama)
12. [Observability & Telemetry](#observability--telemetry)
13. [Key Design Decisions](#key-design-decisions)

---

## Overview

ycode is built around a **RuntimeContext** pattern—no global state. All components receive context explicitly, enabling:
- Deterministic testing
- Nested agent execution
- Clean shutdown
- Resource lifecycle management

### Three Architectural Pillars

1. **Conversation Runtime** - The core event loop that drives LLM interaction
2. **Tool Registry** - Dynamic tool discovery, registration, and execution
3. **Memory Hierarchy** - Five-layer persistence from ephemeral to archival

---

## Repository Layout

```
cmd/ycode/           CLI entry points (cobra commands)
  main.go            Entry point, self-healing wrapper
  serve.go           Background services (API, NATS, dashboard)
  model.go           Ollama/HuggingFace model management
  podman.go          Container CLI commands
  eval.go            Evaluation framework commands
  mesh.go            Agent mesh operations

internal/
  api/               Provider abstraction (Anthropic, OpenAI-compatible)
    provider.go      Model resolution, provider factory
    anthropic.go     Native Anthropic API client
    openai_compat.go OpenAI-compatible provider (xAI, Gemini, Ollama)
    fallback.go      Failover between providers
    key_rotation.go  API key pool with cooldown
    cache_warmer.go  Background prompt cache maintenance

  runtime/           Core runtime systems
    conversation/    Main conversation loop
      runtime.go     Event loop, tool dispatch, state machine
      preactivate.go Deferred tool activation with TTL
    prompt/          Prompt assembly and caching
      builder.go     Static/dynamic section builder
      context.go     Project context assembly
      sections.go    System prompt sections
    memory/          Five-layer memory system
    vfs/             Virtual filesystem with boundary enforcement
    config/          Four-layer configuration merge
    permission/      Approval routing and mode enforcement
    session/         Session persistence and recovery

  tools/             Tool implementations and registry
    registry.go      Tool registry with middleware support
    bash.go          Shell execution with AST safety classification
    vfs_tools.go     File operations (read, write, edit)
    search.go        Code search (grep, semantic, AST)
    web.go           Web fetch and search
    browser.go       Browser automation via browser-use
    agent.go         Sub-agent spawning
    container.go     Container operations
    skill.go         Skill loading and execution
    mcp_tools.go     Model Context Protocol tools
    deferred.go      Deferred tool activation helpers

  container/         Podman container management
    engine.go        Container lifecycle
    pool.go          Container pooling for reuse
    image.go         Image management
    browseruse/      Browser automation service

  inference/         Local LLM inference
    ollama.go        Ollama service component
    runner.go        Embedded runner management
    download.go      Runner binary download

  runtime/swarm/     Agent orchestration
    orchestrator.go  Swarm coordination, agent registry

  mesh/              Self-improving agent mesh
    mesh.go          Mesh lifecycle management
    researcher.go    Research agent implementation
    diagnoser.go     Diagnostic agent implementation
    fixer.go         Auto-fix agent implementation
    learner.go       Learning agent implementation
    safety.go        Safety guardrails

  cli/               Interactive TUI
    app.go           REPL application
    tui.go           Bubble Tea TUI components

  bus/               Event bus for decoupled communication

pkg/ycode/           Public API for embedding
```

---

## Core Runtime Flow

### 1. Entry Point

```
cmd/ycode/main.go → cobra CLI → 
  ├─ REPL mode (interactive TUI)
  └─ One-shot mode (single command)
```

**Self-Healing Wrapper:** Optional panic recovery and token-limit error handling with automatic retry.

### 2. Conversation Loop (`internal/runtime/conversation/runtime.go`)

The heart of ycode—an event-driven loop:

```
┌─────────────────────────────────────────────────────────────┐
│                    CONVERSATION LOOP                         │
├─────────────────────────────────────────────────────────────┤
│  1. ASSEMBLE REQUEST                                         │
│     • Build prompt with static/dynamic sections             │
│     • Load memories (recall via embedding similarity)       │
│     • Activate deferred tools based on context              │
│     • Apply cache control markers (Anthropic)               │
│                                                              │
│  2. SEND TO PROVIDER                                         │
│     • Resolve model alias                                   │
│     • Apply key rotation if needed                          │
│     • Stream events (text/thinking/tool_use)                │
│     • Handle failover on errors                             │
│                                                              │
│  3. DISPATCH TOOL CALLS                                      │
│     • Parse tool_use blocks                                 │
│     • Check permissions (mode + policy)                     │
│     • Execute in parallel (respecting dependencies)         │
│     • Stream results back to provider                       │
│                                                              │
│  4. LOOP UNTIL DONE                                          │
│     • Stop on max_turns, end_turn, or error                 │
│     • Save session state                                    │
│     • Update memory (explicit + auto-extract)               │
└─────────────────────────────────────────────────────────────┘
```

### 3. Pre-Activation of Deferred Tools

Tools can be **deferred** (not sent in every request) to reduce token usage:

```go
// Tool discovery happens via ToolSearch
tools := registry.ToolSearch("metrics") 
// Returns: query_metrics, analyze_dashboard, etc.

// Tools auto-activate when:
// 1. User message contains related keywords
// 2. Similarity score > 0.7 (embedding-based)
// 3. TTL (8 turns) hasn't expired since last use
```

**Implementation:** `internal/runtime/conversation/preactivate.go`

---

## Memory & Context Management

### Five-Layer Memory Hierarchy

```
┌────────────────────────────────────────────────────────────┐
│ LAYER 1: WORKING                                           │
│ • Session message history (in-memory)                      │
│ • Conversation context window                              │
│ • Auto-summarized when token limit approached              │
├────────────────────────────────────────────────────────────┤
│ LAYER 2: SHORT-TERM (Episodic)                             │
│ • JSONL session logs (256KB rotation, 3 backups)           │
│ • Episodic metadata: agent type, tools, duration, outcome  │
│ • Indexed on ycode serve startup                           │
├────────────────────────────────────────────────────────────┤
│ LAYER 3: LONG-TERM (Compaction)                            │
│ • LLM-condensed intent summaries in session JSONL          │
│ • Structured categories: goal, facts, blockers, decisions  │
│ • Triggered at 100K tokens (CompactionThreshold)           │
├────────────────────────────────────────────────────────────┤
│ LAYER 4: CONTEXTUAL (Procedural)                           │
│ • CLAUDE.md/AGENTS.md ancestry file discovery              │
│ • JIT instruction loading when tools access new paths      │
│ • #import directive with circular-reference detection      │
├────────────────────────────────────────────────────────────┤
│ LAYER 5: PERSISTENT                                        │
│ • memory/{name}.md with YAML frontmatter                   │
│ • 7 types × 4 scopes with dynamic value scoring           │
│ • RRF fusion across 4 backends + MMR diversity reranking   │
│ • Entity extraction, temporal validity                     │
│ • Persona: confidence-scored user model (knowledge,        │
│   communication style, behavior, observations)             │
│ • Background dreaming: consolidation every 30 minutes      │
└────────────────────────────────────────────────────────────┘
```

### Memory Scopes

| Scope | Storage Location | Lifetime | Use Case |
|-------|-----------------|----------|----------|
| `global` | `~/.agents/ycode/memory/` | Indefinite | User preferences, coding style |
| `project` | `~/.agents/ycode/projects/{hash}/memory/` | Project lifetime | Project-specific knowledge |
| `team` | (Shared team directory) | Team lifetime | Shared across team members |
| `user` | (Private user directory) | Indefinite | Private to a single user |

### Retrieval Pipeline

Recall uses Reciprocal Rank Fusion (RRF) across four backends — vector, Bleve FTS, keyword, and entity — with composite scoring (recency + dynamic value) and MMR diversity re-ranking. Entity extraction links file paths, URLs, and Go packages to memories for relationship-aware retrieval. Turn-time injection provides per-turn context-aware memory without invalidating the system prompt cache.

### Persona System

A confidence-scored user model (`memory/persona*.go`) infers user identity from environment signals (git user, platform, shell) and passively observes behavioral patterns per turn (message length, question frequency, technical density, corrections, intent). The persona carries five dimensions: knowledge map, communication style, behavior profile, interaction summary, and ephemeral session context. Confidence scales prompt injection linearly (400-char budget). Personas evolve via exponential moving average (α=0.2) at session end and are consolidated during dreaming cycles. Stored as `_persona_{id}.md` in the global memory store. Config: `PersonaEnabled` (default true).

### Prompt Assembly with Static/Dynamic Boundary

```
┌─────────────────────────────────────────────────────────────┐
│ SYSTEM PROMPT                                               │
├─────────────────────────────────────────────────────────────┤
│ [STATIC - Cacheable]                                        │
│ • Base instructions                                         │
│ • AGENTS.md content (with #section tags)                    │
│ • Always-available tool definitions                         │
│ • Skill definitions                                         │
│                                                             │
│ ─────────────────────────────────────────────────────────  │
│                    CACHE BOUNDARY (ephemeral)               │
│ ─────────────────────────────────────────────────────────  │
│                                                             │
│ [DYNAMIC - Per-request]                                     │
│ • Environment state (git status, recent files)              │
│ • Relevant memories (via embedding search)                  │
│ • Persona context (confidence-scaled user directives)       │
│ • Active deferred tools                                     │
│ • Session summary                                           │
│ • Diagnostics (if any)                                      │
└─────────────────────────────────────────────────────────────┘
```

**Implementation:**
- `internal/runtime/prompt/builder.go` - Builder pattern for sections
- `internal/runtime/memory/memory.go` - Manager (Save/Recall/Forget with RRF fusion)
- `internal/runtime/memory/fusion.go` - RRF and MMR algorithms
- `internal/runtime/memory/value.go` - Dynamic value scoring and reward propagation
- `internal/runtime/memory/entity.go` - Entity extraction and linking index
- `internal/runtime/memory/persona.go` - Persona model and session context
- `internal/runtime/prompt/persona.go` - Persona prompt section rendering
- `internal/runtime/memory/profile.go` - Structured user profile
- `internal/runtime/memory/turninjector.go` - Per-turn context-aware injection
- `internal/runtime/memory/temporal.go` - Temporal validity windows
- `internal/runtime/memory/dream.go` - Background consolidation with similarity clustering
- `internal/runtime/memory/reindex.go` - Catch-up indexing on startup
- `internal/runtime/embedding/` - Vector storage and search

---

## Agent Orchestration

### Swarm Orchestrator (`internal/runtime/swarm/orchestrator.go`)

Manages multi-agent coordination:

```go
type Orchestrator struct {
    agentDefs   *AgentRegistry     // Agent definitions from config
    spawner     AgentSpawner       // Creates agent runtime instances
    active      map[string]Agent   // Currently running agents
    results     chan AgentResult   // Result aggregation
}
```

### Agent Mesh (`internal/mesh/`)

Self-improving agent system for autonomous operations:

| Agent | Responsibility | Trigger |
|-------|---------------|---------|
| **Researcher** | Deep research, documentation analysis | Complex questions, API exploration |
| **Diagnoser** | Error analysis, root cause detection | Test failures, build errors |
| **Fixer** | Automated remediation | Diagnostic results |
| **Learner** | Pattern extraction, memory formation | New project structures, conventions |

### Sub-Agent Spawning

Agents can spawn other agents via the `agent` tool:

```json
{
  "type": "researcher",
  "description": "Investigate API usage",
  "prompt": "Research the OpenTelemetry tracing API...",
  "readonly": false,
  "background": false
}
```

**Permission Inheritance:**
- Parent mode constrains subagent (cannot escalate)
- Plan-mode parent forces subagents into plan mode
- Background agents run asynchronously with result polling

---

## Tool System

### Tool Registry (`internal/tools/registry.go`)

Central registry with middleware support:

```go
type Registry struct {
    tools          map[string]ToolSpec
    alwaysAvail    map[string]bool      // Sent every request
    deferred       map[string]ToolSpec  // Discovered on-demand
    middlewares    []Middleware         // Global middleware chain
}
```

### Tool Categories

#### Always-Available Tools (Core)
| Tool | Mode | Description |
|------|------|-------------|
| `bash` | DangerFullAccess | Shell execution with AST safety parsing |
| `read_file` | ReadOnly | Read file contents with offset/limit |
| `write_file` | WorkspaceWrite | Create or overwrite files |
| `edit_file` | WorkspaceWrite | String replacement in files |
| `search` | ReadOnly | Grep search with regex |
| `glob_search` | ReadOnly | File pattern matching |

#### Deferred Tools (Activated on-demand)
| Tool | Category | Activation Trigger |
|------|----------|-------------------|
| `copy/move/delete_file` | File Ops | File manipulation keywords |
| `list_directory/tree` | File Ops | Directory exploration |
| `apply_patch` | File Ops | Diff/patch keywords |
| `web_search` | Web | "search", "find online" |
| `browser_use` | Browser | "browse", "check page" |
| `semantic_search` | Code Intel | "find similar", "semantic" |
| `ast_search` | Code Intel | "structural", "pattern" |
| `goto_definition` | LSP | "definition", "where is" |
| `find_references` | LSP | "references", "used by" |
| `query_metrics/logs` | Observability | "metrics", "logs", "traces" |
| `agent` | Orchestration | "spawn", "delegate" |
| `mcp_*` | MCP | Model Context Protocol tools |

### Tool Deferral System

```
┌─────────────────────────────────────────────────────────────┐
│ DEFERRED TOOL LIFECYCLE                                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  DISCOVERED ──► ACTIVATED ──► USED ──► COOLDOWN ──► RESET  │
│      ▲              │            │           │              │
│      │              │            │           │              │
│      └──────────────┴────────────┴───────────┘              │
│                    (TTL = 8 turns)                          │
│                                                             │
│  Activation triggers:                                       │
│  • Keyword match in user query                              │
│  • Embedding similarity > 0.7                               │
│  • Explicit request via ToolSearch                          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Bash Safety Classification (`internal/tools/bash.go`)

Shell commands are analyzed via AST parsing:

```go
type SafetyLevel int
const (
    SafeReadOnly SafetyLevel = iota  // ls, cat, grep, find
    SafeWorkspaceWrite               // go build, go test
    RequiresConfirmation             // git push, docker rm
    KnownDangerous                   // rm -rf, dd, mkfs
)
```

Commands with `SafetyLevel > SafeWorkspaceWrite` require user confirmation or `--danger` flag.

---

## LLM Provider Layer

### Provider Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    PROVIDER STACK                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              InstrumentedProvider                    │   │
│  │  • OTEL tracing and metrics                         │   │
│  │  • Cost estimation                                  │   │
│  └─────────────────────────────────────────────────────┘   │
│                         ▲                                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              FallbackProvider                        │   │
│  │  • Primary → Secondary → Tertiary failover          │   │
│  │  • Error classification (rate limit, auth, etc.)    │   │
│  └─────────────────────────────────────────────────────┘   │
│                         ▲                                   │
│  ┌─────────────────────┬─────────────────────────────┐     │
│  │                     │                             │     │
│  ▼                     ▼                             ▼     │
│ ┌─────────┐      ┌──────────┐                ┌──────────┐ │
│ │Anthropic│      │OpenAICompat│              │  Ollama  │ │
│ │  Native │      │(OpenAI, xAI,│             │ (Local)  │ │
│ │         │      │ Gemini)    │              │          │ │
│ └─────────┘      └──────────┘                └──────────┘ │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Provider Capabilities

Detected per model:
- **Caching** - Prompt caching support (Anthropic)
- **Thinking** - Extended thinking/reasoning mode
- **Vision** - Image input support
- **ToolUse** - Function calling support
- **MaxTokens** - Context window size

### Model Aliases

```go
var ModelAliases = map[string]string{
    "claude-opus":   "claude-3-opus-20240229",
    "claude-sonnet": "claude-3-7-sonnet-20250219",
    "claude-haiku":  "claude-3-5-haiku-20241022",
    "gpt-4":         "gpt-4o",
    "local":         "llama3.2",
}
```

Resolution order: CLI flag → Config alias → Direct model ID → Provider detection

---

## Configuration System

### Four-Layer Config Merge

Configs are merged in order (later overrides earlier):

```
1. ~/.config/ycode/settings.json          (User defaults)
         ↓
2. <project>/.agents/ycode/settings.json  (Project defaults)
         ↓
3. <cwd>/.agents/ycode/settings.json      (Local overrides)
         ↓
4. settings.local.json                    (Gitignored secrets)
```

### Merge Behavior

| Field | Behavior |
|-------|----------|
| `Instructions` | Append (cumulative) |
| `AllowedDirectories` | Append (union) |
| All others | Override (replace) |

### Key Configuration Sections

```json
{
  "model": "claude-sonnet-4-20250514",
  "provider": "anthropic",
  "mode": "workspace-write",
  "instructions": ["Custom system prompt additions"],
  "allowed_directories": ["/home/user/projects"],
  "tools": {
    "disabled": ["bash"],
    "deferred_defaults": ["web_search", "browser_use"]
  },
  "agents": {
    "researcher": {
      "model": "claude-opus",
      "mode": "read-only"
    }
  },
  "containers": {
    "enabled": true,
    "sandbox": true
  }
}
```

---

## Permission & Security Model

### Three-Tier Permission Mode

```
┌────────────────────────────────────────────────────────────────┐
│ DANGER-FULL-ACCESS                                             │
│ • Shell execution (bash)                                       │
│ • Process control                                              │
│ • MCP tool execution                                           │
│ • Container privileged operations                              │
├────────────────────────────────────────────────────────────────┤
│ WORKSPACE-WRITE (Default)                                      │
│ • File read/write/edit within allowed directories              │
│ • Git operations                                               │
│ • Build/test commands                                          │
│ • Web search and browser use                                   │
├────────────────────────────────────────────────────────────────┤
│ READ-ONLY                                                      │
│ • File reading                                                 │
│ • Search and code navigation                                   │
│ • Plan mode (dry-run)                                          │
└────────────────────────────────────────────────────────────────┘
```

### Approval Routing (`internal/runtime/permission/`)

```
┌─────────────────────────────────────────────────────────────┐
│                    APPROVAL FLOW                             │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Tool Request                                               │
│      │                                                      │
│      ▼                                                      │
│  ┌─────────────────┐    Blocked    ┌─────────────┐         │
│  │ Policy Override │ ─────────────►│   Deny      │         │
│  │ (allow/block)   │               │   Access    │         │
│  └─────────────────┘               └─────────────┘         │
│      │ Allowed                                               │
│      ▼                                                      │
│  ┌─────────────────┐    No      ┌─────────────┐             │
│  │ Mode Sufficient │───────────►│ User Prompt │             │
│  │ (>= required)   │            │ (interactive)│             │
│  └─────────────────┘    Yes     └─────────────┘             │
│      │                              │                       │
│      │ Yes                          │ Approve               │
│      ▼                              ▼                       │
│  ┌─────────────────────────────────────────────┐            │
│  │              EXECUTE TOOL                    │            │
│  └─────────────────────────────────────────────┘            │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### VFS Path Enforcement

Every file operation passes through `internal/runtime/vfs/vfs.go`:

1. **Absolute path resolution** - All paths converted to absolute
2. **Allowed directory check** - Path must be within allowed roots
3. **Symlink resolution** - Symlinks resolved and validated
4. **Escape prevention** - `..` traversal blocked at boundary

---

## Container & Browser Infrastructure

### Podman Integration

ycode uses Podman for:
- **Sandboxed execution** - Untrusted code in isolated containers
- **Browser automation** - browser-use library in container
- **Tool isolation** - AST search, semantic analysis

```
┌─────────────────────────────────────────────────────────────┐
│                   CONTAINER ARCHITECTURE                     │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ycode CLI                                                  │
│      │                                                      │
│      ▼                                                      │
│  ┌─────────────────────────────────────────────────────┐   │
│  │          Container Engine (Podman)                   │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐ │   │
│  │  │   Browser   │  │   Sandbox   │  │   Search    │ │   │
│  │  │   Service   │  │   Runner    │  │   Service   │ │   │
│  │  │  (Chrome)   │  │  (isolated) │  │ (ast-grep)  │ │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘ │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  Container lifecycle:                                       │
│  • Auto-start on first use                                  │
│  • Connection pooling for reuse                             │
│  • Health checks and auto-restart                           │
│  • Cleanup on shutdown                                      │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Browser Automation (`internal/runtime/browseruse/`)

Uses the [browser-use](https://github.com/browser-use/browser-use) Python library:

```python
# Containerized execution
browser-use navigate --url "https://example.com"
browser-use click --selector "button#submit"
browser-use extract --selector "div.content"
```

Actions supported:
- `navigate` - Load URL
- `click` - Element interaction
- `type` - Text input
- `extract` - Content extraction
- `screenshot` - Visual capture

---

## Local LLM Integration (Ollama)

### Embedded Runner

ycode can run Ollama directly without external installation:

```
┌─────────────────────────────────────────────────────────────┐
│                    OLLAMA INTEGRATION                        │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │           RunnerManager (internal/inference/)        │   │
│  │                                                      │   │
│  │  1. Check embedded runner (bin/ollama-runner)       │   │
│  │  2. Check system PATH for ollama                    │   │
│  │  3. Check OLLAMA_RUNNERS env var                    │   │
│  │  4. Auto-download if needed                         │   │
│  │                                                      │   │
│  │  Port: Dynamic (finds available)                    │   │
│  │  Data: ~/.config/ycode/ollama/                      │   │
│  └─────────────────────────────────────────────────────┘   │
│                         │                                   │
│                         ▼                                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              Ollama API (local)                      │   │
│  │  • Model management (pull, list, delete)            │   │
│  │  • Inference endpoint                               │   │
│  │  • Embedding generation                             │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Model Management Commands

```bash
ycode model pull llama3.2          # Pull from Ollama registry
ycode model pull --hf repo/model   # Pull from HuggingFace
ycode model list                   # List available models
ycode model delete model_name      # Remove model
```

---

## Observability & Telemetry

### OpenTelemetry Integration

ycode exports OTEL data to:
- **OTEL Collector** (if configured)
- **Local files** (JSONL format for offline analysis)
- **VictoriaLogs/Jaeger** (when `ycode serve` is running)

### Key Metrics

| Metric | Description |
|--------|-------------|
| `api.requests.total` | LLM API call count |
| `api.tokens.input` | Input tokens consumed |
| `api.tokens.output` | Output tokens generated |
| `api.cost.estimated` | Estimated API cost |
| `tools.calls.total` | Tool invocation count |
| `tools.duration` | Tool execution time |
| `conversation.turns` | Turns per session |
| `cache.hits/misses` | Prompt cache efficiency |

### Session Transcript Export

```bash
ycode serve export --session <id> --format json
```

Exports full conversation with:
- Message history
- Tool calls and results
- Token usage
- Timing information
- Memory formations

---

## Key Design Decisions

### 1. RuntimeContext Over Global State

All components receive `*RuntimeContext` explicitly:
- Deterministic testing
- Parallel conversation support
- Clean resource lifecycle

### 2. Static/Dynamic Prompt Boundary

Optimizes for providers with prompt caching:
- Static sections cached (AGENTS.md, tool defs)
- Dynamic sections regenerated per-request
- Cache control markers for Anthropic

### 3. Deferred Tool Activation

Reduces token usage and improves focus:
- Only relevant tools sent to LLM
- TTL-based activation (8 turns)
- Keyword + embedding activation triggers

### 4. Permission Mode Hierarchy

Three clear levels with no exceptions:
- Cannot escalate from parent to subagent
- Plan mode is infectious
- VFS enforces boundaries regardless of mode

### 5. Container-First Architecture

Heavy dependencies isolated in containers:
- Browser automation
- AST analysis tools
- Sandbox execution
- Keeps binary small, dependencies minimal

### 6. Local-First LLM Support

Embedded Ollama runner:
- No external dependencies for local inference
- Automatic fallback from cloud to local
- Privacy-sensitive operations stay local

---

## Dependencies

| Component | Import Path | License |
|-----------|-------------|---------|
| Anthropic SDK | `github.com/anthropics/anthropic-sdk-go` | MIT |
| OpenTelemetry | `go.opentelemetry.io/otel/*` | Apache-2.0 |
| Bubble Tea (TUI) | `github.com/charmbracelet/bubbletea` | MIT |
| Cobra (CLI) | `github.com/spf13/cobra` | Apache-2.0 |
| Bleve (search) | `github.com/blevesearch/bleve/v2` | Apache-2.0 |
| Podman | `go.podman.io/podman/v6` | Apache-2.0 |
| NATS | `github.com/nats-io/nats.go` | Apache-2.0 |

---

## Extension Points

### Adding a New Tool

```go
// internal/tools/my_tool.go
func RegisterMyTool(r *tools.Registry) {
    r.Register(tools.ToolSpec{
        Name:        "my_tool",
        Description: "Does something useful",
        InputSchema: generateSchema(MyInput{}),
        Handler:     handleMyTool,
        RequiredMode: permission.WorkspaceWrite,
    })
}
```

### Adding a New Provider

Implement the `api.Provider` interface:

```go
type Provider interface {
    Send(ctx context.Context, req *Request) (chan *StreamEvent, chan error)
    Kind() ProviderKind
}
```

### Adding a New Skill

Create `skills/<name>/skill.md`:

```yaml
---
name: my-skill
description: Does something awesome
---

## System Prompt Addition

When this skill is active, add this to the system prompt...

## Available Tools

- `my_custom_tool`: Description
```

---

*Architecture documentation version 2.0 - reflects ycode as of current main branch*

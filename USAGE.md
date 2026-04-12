# ycode Usage

This guide covers the `ycode` CLI binary. If you are new, start with `ycode doctor` to verify your setup.

## Quick-start health check

```bash
go build -o bin/ycode ./cmd/ycode/
./bin/ycode doctor
```

## Authentication

### Anthropic API key

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
```

### OpenAI-compatible providers

```bash
# OpenAI
export OPENAI_API_KEY="sk-..."

# Custom endpoint (Ollama, LM Studio, etc.)
export OPENAI_API_KEY="local-dev-token"
export OPENAI_BASE_URL="http://localhost:11434/v1"

# OpenRouter
export OPENAI_API_KEY="sk-or-v1-..."
export OPENAI_BASE_URL="https://openrouter.ai/api/v1"
```

## CLI modes

### Interactive REPL

```bash
ycode
```

Starts an interactive session. Type `/help` for available commands, `/quit` to exit.

### One-shot prompt

```bash
ycode prompt "summarize this repository"
```

### Piped input

```bash
echo "explain this code" | ycode
cat prompt.txt | ycode
git diff | ycode --print "review these changes"
```

The `--print` flag outputs plain text without markdown rendering, useful for scripting.

### Continuous loop

```bash
# CLI subcommand
ycode loop --interval 5m --prompt review-prompt.md

# Or via slash command in REPL
/loop 5m /review
/loop stop
```

Reads the prompt file each iteration, so edits take effect on the next run. Press Ctrl+C to stop.

## Slash commands

### Session

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/status` | Show session status (ID, messages, model) |
| `/cost` | Show token usage and cost |
| `/version` | Show version |
| `/clear` | Clear conversation history |
| `/compact` | Compact conversation by summarizing older messages |

### Workspace

| Command | Description |
|---------|-------------|
| `/config` | View or modify configuration |
| `/memory` | View or manage memories |
| `/init` | Initialize ycode for current project |

### Discovery

| Command | Description |
|---------|-------------|
| `/doctor` | Run health checks |
| `/context` | Show current context (instruction files, git, etc.) |
| `/skills` | List available skills |
| `/tasks` | List running tasks |

### Automation

| Command | Description |
|---------|-------------|
| `/review [scope]` | Review code changes (staged, commit, branch) |
| `/advisor [topic]` | Get architectural advice or codebase insights |
| `/security-review [scope]` | Run security analysis on code changes |
| `/team list\|create\|delete` | Manage parallel agent teams |
| `/cron list\|create\|delete` | Manage scheduled recurring tasks |
| `/loop [interval] [command]` | Run a command on a recurring interval |

### Plugins

| Command | Description |
|---------|-------------|
| `/plugin list` | List installed plugins |
| `/plugin install <name>` | Install a plugin |
| `/plugin enable <name>` | Enable a plugin |
| `/plugin disable <name>` | Disable a plugin |
| `/plugin uninstall <name>` | Uninstall a plugin |
| `/plugin update [name]` | Update plugin(s) |

## Configuration

Config is loaded from three tiers (later overrides earlier):

1. `~/.config/ycode/settings.json` (user)
2. `.ycode/settings.json` (project)
3. `.ycode/settings.json` (local/CWD)

### Settings

```json
{
  "model": "claude-sonnet-4-20250514",
  "maxTokens": 8192,
  "permissionMode": "ask",
  "autoMemoryEnabled": true,
  "autoCompactEnabled": true,
  "autoDreamEnabled": false,
  "fileCheckpointingEnabled": false
}
```

### Permission modes

| Mode | Description |
|------|-------------|
| `ask` | Ask before running tools that modify files or execute commands |
| `read-only` | Only allow read operations |
| `workspace-write` | Allow file modifications within the workspace |
| `danger-full-access` | Allow all operations without prompting |

## Tools

ycode provides 50+ tools organized by category:

### Core file operations
`bash`, `read_file`, `write_file`, `edit_file`, `glob_search`, `grep_search`

### Web
`WebFetch`, `WebSearch`

### Interaction
`AskUserQuestion`, `SendUserMessage`, `TodoWrite`, `Skill`

### Agent & task management
`Agent`, `TaskCreate`, `TaskGet`, `TaskList`, `TaskUpdate`, `TaskStop`, `TaskOutput`

### Worker management
`WorkerCreate`, `WorkerGet`, `WorkerObserve`, `WorkerResolveTrust`, `WorkerAwaitReady`, `WorkerSendPrompt`, `WorkerRestart`, `WorkerTerminate`, `WorkerObserveCompletion`

### Team & scheduling
`TeamCreate`, `TeamDelete`, `CronCreate`, `CronDelete`, `CronList`

### Code intelligence
`LSP`, `NotebookEdit`

### External integration
`MCP`, `ListMcpResources`, `ReadMcpResource`, `McpAuth`, `RemoteTrigger`

### Configuration & mode
`Config`, `EnterPlanMode`, `ExitPlanMode`

### Utility
`Sleep`, `REPL`, `PowerShell`, `StructuredOutput`, `ToolSearch`

Tools are split into **always-available** (bash, read_file, write_file, edit_file, glob_search, grep_search) and **deferred** (discovered via ToolSearch on demand).

## Memory system

ycode has a multi-layered memory system:

- **Working memory**: current conversation context window
- **Short-term memory**: JSONL session files, rotate at 256KB
- **Long-term memory**: auto-compaction at 100K tokens with semantic summary
- **Contextual memory**: CLAUDE.md instruction files discovered from CWD to root
- **Persistent memory**: file-based memories in `~/.ycode/projects/{hash}/memory/` with MEMORY.md index

Memory types: `user`, `feedback`, `project`, `reference`

Auto-dream mode (`autoDreamEnabled`) consolidates memories in the background, removing stale entries and merging similar project memories.

## Session management

Sessions are persisted as JSONL files. Auto-compaction produces semantic summaries preserving:
- Message scope (user/assistant/tool counts)
- Tools mentioned
- Recent user requests (last 3)
- Pending work items
- Key files referenced (up to 8)
- Current work status

## Skills

Skills are discovered from a hierarchy of directories:

1. `.ycode/skills/` in project ancestors (CWD to root)
2. `~/.ycode/skills/` (home)
3. `$YCODE_SKILLS_DIR` (environment variable)

Each skill is a directory containing:
- `SKILL.md` -- instructions with YAML frontmatter
- `scripts/` -- optional executable scripts
- `resources/` -- data files, templates, examples

Bundled skills: `remember`, `loop`, `simplify`, `review`, `commit`, `pr`

Install bundled skills via `/skills install-bundled`.

## Agent types

When spawning sub-agents, each type gets a tailored tool allowlist:

| Type | Tools |
|------|-------|
| `Explore` | read-only (read_file, glob, grep, WebFetch, WebSearch, ToolSearch, Skill) |
| `Plan` | Explore + TodoWrite, SendUserMessage |
| `Verification` | Plan + bash, write_file, edit_file, REPL, PowerShell |
| `general-purpose` | All common tools |
| `claw-guide` | Read-only + messaging |
| `statusline-setup` | read_file, edit_file, Config |

Agents can recursively spawn child agents up to a configurable depth (default: 3).

## Embedding

ycode can be embedded as a library:

```go
import "github.com/qiangli/ycode/pkg/ycode"

agent, err := ycode.NewAgent(
    ycode.WithModel("claude-sonnet-4-20250514"),
    ycode.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
)
if err != nil {
    log.Fatal(err)
}
result, err := agent.Run(ctx, "summarize this file")
```

## Cross-compilation

```bash
make cross
```

Produces binaries in `dist/`:
- `ycode-linux-amd64`
- `ycode-linux-arm64`
- `ycode-darwin-amd64`
- `ycode-darwin-arm64`
- `ycode-windows-amd64.exe`

Version and commit are injected via `-ldflags`:

```bash
go build -ldflags "-X main.version=v1.0.0 -X main.commit=$(git rev-parse --short HEAD)" ./cmd/ycode/
```

## Development Workflow: Build → Deploy → Validate

Three Makefile targets and matching skill definitions (`skills/{build,deploy,validate}/skill.md`) form the standard development cycle. Each step depends on the previous one succeeding. Any AI agent or human developer should follow this same workflow.

### Build

**Target**: `make build`

Runs the full quality gate: `go mod tidy` → `go fmt` → `go vet` → `go test -race` → `go build` → `bin/ycode version`.

**On failure**: Diagnose the error, fix the source, and re-run `make build` from the top. The entire pipeline must pass end-to-end — do not skip steps. Allow up to 3 fix-and-retry cycles before escalating.

**On success**: If any files changed (fixes, formatting, go.sum updates), stage and commit them with a descriptive message. If the tree is clean, skip the commit.

Quick compile without checks: `make compile`

### Deploy

**Target**: `make deploy` (localhost:58080 by default)

**Pre-requisite**: `make build` must have succeeded. Do not deploy a broken build.

Kills any existing instance on the target port, starts `ycode serve --detach`, and verifies the health endpoint.

```bash
# Localhost (default)
make deploy

# Custom port
make deploy PORT=9090

# Remote host
make deploy HOST=myserver PORT=58080
```

For remote hosts, passwordless SSH must be configured. If `ssh -o BatchMode=yes <host> "echo ok"` fails, set it up:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519 -N ""   # if no key exists
ssh-copy-id <host>                                    # one-time password prompt
ssh -o BatchMode=yes <host> "echo ok"                 # verify
```

Remote deploy auto-detects architecture and cross-compiles if the remote platform differs from the local one.

### Validate

**Target**: `make validate` (localhost:58080 by default)

**Pre-requisites**: `/build` then `/deploy` must have succeeded.

Runs four test suites against a running ycode instance:

1. **Smoke tests** — healthz, dashboard, version, server status
2. **Integration tests** — OTEL collector, Prometheus metrics, trace/metric/log ingestion, proxy routing
3. **Acceptance tests** — one-shot prompt, serve subcommands, doctor
4. **Performance tests** — healthz latency (p50/p95/p99), trace ingestion throughput, binary startup time

```bash
# Localhost (default)
make validate

# Remote
make validate HOST=myserver PORT=58080
```

**On failure**: Diagnose which suite/test failed. Fix the root cause in source, then repeat the full cycle: build → deploy → validate. Allow up to 3 fix-and-retry cycles before escalating.

**On success**: Reports a summary with pass/fail/skip counts and performance baselines.

### Full cycle example

```bash
make build              # quality gate + commit fixes
make deploy             # start server
make validate           # run test suites

# Or remote
make build
make deploy HOST=staging PORT=58080
make validate HOST=staging PORT=58080
```

## Architecture

The codebase follows a standard Go layout with `cmd/` for binaries, `internal/` for private packages, and `pkg/` for public API.

### Key runtime flow

1. **Entry**: `cmd/ycode/main.go` → cobra CLI → either interactive REPL (`internal/cli/app.go`) or one-shot mode
2. **Conversation loop**: `internal/runtime/conversation/runtime.go` assembles API requests, sends to provider, dispatches tool calls via `ToolExecutor`
3. **System prompt**: `internal/runtime/prompt/builder.go` assembles sections with a static/dynamic boundary for cache optimization
4. **Tool dispatch**: `internal/tools/registry.go` maps tool names to handlers (always-available or deferred via ToolSearch)
5. **Session**: `internal/runtime/session/` persists conversations as JSONL, with auto-compaction at 100K tokens

### Provider layer (`internal/api/`)

- `client.go` — `Provider` interface (Send, Kind)
- `anthropic.go` — Anthropic API with SSE streaming
- `openai_compat.go` — OpenAI-compatible providers (OpenAI, xAI, Ollama, etc.)
- `prompt_cache.go` — prompt fingerprinting for cache hit detection

### Memory system (`internal/runtime/memory/`)

Five layers: working (context window) → short-term (session JSONL) → long-term (compaction summaries) → contextual (CLAUDE.md ancestry) → persistent (file-based `~/.ycode/projects/`). Types: user, feedback, project, reference.

### Config (`internal/runtime/config/`)

Three-tier merge: user (`~/.config/ycode/settings.json`) > project (`.ycode/settings.json`) > local (`.ycode/settings.local.json`).

### Permission (`internal/runtime/permission/`)

Modes: ReadOnly, WorkspaceWrite, DangerFullAccess. Each tool declares its required level.

## Dependencies

Only permissive licenses (MIT, Apache-2.0, BSD). Key deps: cobra (CLI), bubbletea (TUI), glamour (markdown), chroma (syntax highlighting), uuid. Go stdlib for everything else (Go 1.25+).

## Key Design Decisions

- Map-based ToolRegistry with runtime registration (plugins/MCP add tools without recompilation)
- `RuntimeContext` struct holds all registries — no global state
- `context.Context` propagation everywhere for cancellation/timeout
- JSONL sessions for interop with priorart/clawcode format
- Section-based prompt assembly with dynamic boundary marker for cache optimization
- Per-tool middleware for permission, logging, timing as composable wrappers
- Recursive agent delegation up to configurable depth (default: 3)

## Verification

```bash
go build ./cmd/ycode/      # compiles
go test -race ./...         # all tests pass
go vet ./...                # no issues
ycode doctor                # health checks pass
ycode prompt "hello"        # one-shot works
```

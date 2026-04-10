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

## Verification

```bash
go build ./cmd/ycode/      # compiles
go test -race ./...         # all tests pass
go vet ./...                # no issues
ycode doctor                # health checks pass
ycode prompt "hello"        # one-shot works
```

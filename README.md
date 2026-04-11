# ycode

A pure Go CLI agent harness for autonomous software development. ycode provides 50+ tools, MCP/LSP integration, a plugin system, permission enforcement, multi-layered memory, and session management.

ycode is a ground-up rewrite of [priorart/clawcode (Claw Code)](https://github.com/ultraworkers/claw-code) in Go, with only permissive-license dependencies (MIT, Apache-2.0, BSD).

## Quick start

```bash
# 1. Build
go build -o bin/ycode ./cmd/ycode/

# 2. Set your API key
export ANTHROPIC_API_KEY="sk-ant-..."

# 3. Health check
./bin/ycode doctor

# 4. One-shot prompt
./bin/ycode prompt "say hello"

# 5. Interactive REPL
./bin/ycode
```

## Features

- **50+ tools**: file operations, bash execution, web fetch/search, code intelligence, agent spawning, task management, and more
- **MCP integration**: connect to MCP servers (stdio transport), expose ycode as an MCP server
- **LSP integration**: hover, definition, references, symbols, diagnostics via language servers
- **Plugin system**: install, enable, disable, uninstall plugins with hook lifecycle (PreToolUse, PostToolUse, PostToolUseFailure)
- **Permission enforcement**: three modes (ReadOnly, WorkspaceWrite, DangerFullAccess) with policy rules
- **Multi-layered memory**: working, short-term (session), long-term (compaction), contextual (instruction files), persistent (file-based)
- **Session management**: JSONL persistence, auto-compaction at 100K tokens, semantic summary extraction, session resume
- **Prompt caching**: fingerprint-based cache with TTL, break detection
- **Continuous loop mode**: `/loop` command and `ycode loop` CLI for recurring agent execution
- **Scratchpad**: markdown working memory with checkpoints and work logs
- **Agent delegation**: recursive child agent spawning with configurable depth
- **Auto-research**: autonomous query decomposition into parallel sub-tasks
- **Skills system**: hierarchical discovery, executable scripts, bundled skills (review, commit, pr, simplify, loop, remember)
- **OpenAI-compatible provider**: works with OpenAI, xAI/Grok, DashScope/Qwen, Ollama, OpenRouter
- **Embeddable**: `pkg/ycode/` provides a public Go API for embedding ycode as a library
- **Cross-platform**: builds for Linux, macOS, and Windows (amd64 and arm64)

## Prerequisites

- Go 1.24+
- One of:
  - `ANTHROPIC_API_KEY` for Anthropic models
  - `OPENAI_API_KEY` (+ optional `OPENAI_BASE_URL`) for OpenAI-compatible models

## Build & test

```bash
go build ./cmd/ycode/          # build
go test -race ./...             # test with race detector
go vet ./...                    # static analysis
make cross                      # cross-compile all platforms
```

## Documentation

- [USAGE.md](./USAGE.md) -- CLI commands, configuration, sessions, tools, and workflows
- [docs/plan.md](./docs/plan.md) -- architecture, design decisions, and project structure
- [docs/todo.md](./docs/todo.md) -- implementation checklist

## Project structure

```
cmd/ycode/                  Main binary entry point (cobra CLI)
internal/
  api/                      Provider clients (Anthropic, OpenAI-compatible), SSE streaming
  cli/                      REPL, terminal rendering, input handling
  commands/                 30+ slash commands
  plugins/                  Plugin manager, hooks
  runtime/
    bash/                   Bash execution + validation
    config/                 3-tier config merge (user > project > local)
    conversation/           Turn loop, tool execution, delegation, research
    fileops/                read, write, edit, glob, grep
    git/                    Git context, branch lock, stale detection
    hooks/                  Hook runner
    loop/                   Continuous agent loop, scheduler, file watcher
    lsp/                    LSP client registry and actions
    mcp/                    MCP client/server, stdio transport, tool bridge
    memory/                 Multi-layered memory system with auto-dream
    oauth/                  PKCE OAuth flow
    permission/             Modes, policy, enforcer
    policy/                 Policy engine with lane events
    prompt/                 Section-based system prompt assembly
    recovery/               Recovery recipes
    sandbox/                Container detection, Linux namespace sandbox
    scratchpad/             Markdown working memory, checkpoints, work log
    session/                JSONL persistence, compaction, summary
    task/                   Background task registry
    team/                   Team + cron registries
    usage/                  Token/cost tracking
    worker/                 Worker boot lifecycle, events
  telemetry/                Sinks, tracing, analytics
  tools/                    50+ tool specs, registry, dispatch, handlers
  testutil/                 Mock API server for testing
pkg/ycode/                  Public embedding API
```

## Improvements over priorart/clawcode

- **Pure Go** with permissive-only dependencies
- **Embeddable library API** via `pkg/ycode/`
- **No global state** -- all registries on context structs
- **Runtime tool registration** -- plugins/MCP add tools without recompilation
- **Full memory subsystem** -- multi-layered with auto-dream consolidation
- **Continuous loop** -- real `/loop` command with background scheduler
- **Markdown scratchpad** -- checkpoints, work logs, auto-checkpoint on compaction
- **Recursive agent delegation** -- agents spawn child agents up to configurable depth
- **Executable skills** -- scripts and resources alongside markdown instructions

## Prior art & acknowledgments

ycode draws inspiration from several excellent open-source projects, included as git submodules under `priorart/` for reference:

| Project | License | Description |
|---------|---------|-------------|
| [Aider](https://github.com/aider-ai/aider) | Apache-2.0 | AI pair programming in the terminal |
| [Claw Code](https://github.com/ultraworkers/claw-code) | -- | Rust-based CLI agent harness (direct ancestor of ycode) |
| [Cline](https://github.com/cline/cline) | Apache-2.0 | Autonomous coding agent for IDEs |
| [Codex](https://github.com/openai/codex) | Apache-2.0 | OpenAI's CLI coding agent |
| [Continue](https://github.com/continuedev/continue) | Apache-2.0 | Open-source AI code assistant |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Apache-2.0 | Google's CLI for Gemini models |
| [OpenClaw](https://github.com/openclaw/openclaw) | MIT | Open-source CLI agent harness |
| [OpenCode](https://github.com/anomalyco/opencode) | MIT | Terminal-based AI coding assistant |
| [OpenHands](https://github.com/OpenHands/OpenHands) | MIT | Platform for AI software agents |

We are grateful to the authors and communities behind these projects.

## License

MIT

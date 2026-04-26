# ycode

A pure Go CLI agent harness for autonomous software development. Single static binary, permissive-license dependencies only (MIT, Apache-2.0, BSD).

## Quick start

```bash
make init                              # initialize submodules (first time only)
make build                             # full quality gate
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY
./bin/ycode doctor                     # health check
./bin/ycode                            # interactive REPL
```

## Features

- **50 tools**: file operations, bash execution, web fetch/search, code intelligence, git operations, agent spawning, task management, memory, observability queries
- **Multi-provider LLM**: Anthropic (native), OpenAI-compatible (OpenAI, xAI/Grok, DashScope/Qwen, Ollama, OpenRouter)
- **Local inference**: embedded Ollama runner, HuggingFace GGUF model support
- **Serve mode**: HTTP/WebSocket/NATS server (`ycode serve`) with embedded observability (Prometheus, Jaeger, VictoriaLogs, Perses)
- **Container management**: embedded Podman integration for sandbox and workspace isolation
- **Git server**: embedded Gitea for agent workspace operations
- **MCP/LSP integration**: MCP client/server (stdio transport), LSP for code intelligence
- **Plugin system**: manifest-based plugins with hook lifecycle (PreToolUse, PostToolUse)
- **Permission enforcement**: three modes (ReadOnly, WorkspaceWrite, DangerFullAccess) with policy rules
- **Multi-layered memory**: working, short-term (session), long-term (compaction), contextual (instruction files), persistent (file-based), plus Memos integration
- **Session management**: JSONL persistence, auto-compaction at 100K tokens, semantic summaries
- **Agent delegation**: recursive child agent spawning with configurable depth, team and cron management
- **Skills system**: hierarchical discovery, bundled skills (review, commit, pr, simplify, loop, remember)
- **Self-healing**: automatic error recovery with classification and retry
- **Chat hub**: multi-channel messaging bridges
- **OAuth login**: PKCE flow for Claude authentication
- **Embeddable**: `pkg/ycode/` provides a public Go API
- **Cross-platform**: Linux, macOS, Windows (amd64 and arm64)

## Prerequisites

- Go 1.26+
- One of:
  - `ANTHROPIC_API_KEY` for Anthropic models
  - `OPENAI_API_KEY` (+ optional `OPENAI_BASE_URL`) for OpenAI-compatible models

## Documentation

- [AGENTS.md](./AGENTS.md) -- instructions for AI coding assistants (CLAUDE.md symlinks here)
- [docs/usage.md](./docs/usage.md) -- CLI modes, configuration, tools, and workflows
- [docs/instructions.md](./docs/instructions.md) -- conventions, skill system, build/test/commit rules
- [docs/architecture.md](./docs/architecture.md) -- full architecture, design decisions, component details

## Prior art & acknowledgments

ycode is a ground-up rewrite of [Claw Code](https://github.com/ultraworkers/claw-code) in Go, drawing inspiration from several open-source projects included as submodules under `priorart/`:

| Project | License | Description |
|---------|---------|-------------|
| [Aider](https://github.com/aider-ai/aider) | Apache-2.0 | AI pair programming in the terminal |
| [Claw Code](https://github.com/ultraworkers/claw-code) | -- | Rust-based CLI agent harness (direct ancestor) |
| [Cline](https://github.com/cline/cline) | Apache-2.0 | Autonomous coding agent for IDEs |
| [Codex](https://github.com/openai/codex) | Apache-2.0 | OpenAI's CLI coding agent |
| [Continue](https://github.com/continuedev/continue) | Apache-2.0 | Open-source AI code assistant |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Apache-2.0 | Google's CLI for Gemini models |
| [OpenClaw](https://github.com/openclaw/openclaw) | MIT | Open-source CLI agent harness |
| [OpenCode](https://github.com/anomalyco/opencode) | MIT | Terminal-based AI coding assistant |
| [OpenHands](https://github.com/OpenHands/OpenHands) | MIT | Platform for AI software agents |

## License

MIT

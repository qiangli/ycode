# ycode

A pure Go CLI agent harness for autonomous software development. Single static binary, permissive-license dependencies only (MIT, Apache-2.0, BSD).

## Install

### Homebrew (macOS / Linux, once tap is bootstrapped)

```bash
brew tap qiangli/ycode
brew install ycode
```

(Tap bootstrap is a one-time admin step — see [docs/release.md](./docs/release.md#homebrew-tap).)

### Prebuilt binary

Latest release: <https://github.com/qiangli/ycode/releases/latest>

```bash
# macOS (Apple Silicon)
curl -sSL https://github.com/qiangli/ycode/releases/latest/download/ycode-darwin-arm64.tar.gz | tar -xz
sudo mv ycode /usr/local/bin/

# Linux (x86_64)
curl -sSL https://github.com/qiangli/ycode/releases/latest/download/ycode-linux-amd64.tar.gz | tar -xz
sudo mv ycode /usr/local/bin/
```

Each release includes a `SHA256SUMS` file alongside the archives. Verify before installing:

```bash
curl -sSLO https://github.com/qiangli/ycode/releases/latest/download/SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Other platforms (darwin-amd64, linux-arm64, windows) are not yet packaged — see `.github/workflows/release.yml` for the matrix and the open issues blocking each.

### From source

```bash
git clone https://github.com/qiangli/ycode.git
cd ycode
make init   # initialize submodules (first time only)
make build  # full quality gate; binary lands at bin/ycode
```

## Quick start

```bash
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY
ycode doctor                            # health check
ycode                                   # interactive REPL
```

## Features

The list below is auto-generated from `internal/features/registry.yaml` (filtered to `tier: stable`). To update, edit the registry and run `make readme-features`.

<!-- BEGIN FEATURES -->
- **bash-tool** — in-process bash via mvdan/sh — no host shell exec, with security middleware (Setpgid, pre-exec validation)
- **file-ops** — read, write, edit, glob — file operations bounded by the per-session VFS
- **search-tools** — Grep, Glob, semantic search, symbol search, AST search across the project
- **web-tools** — WebFetch, WebSearch — auto-routes between Brave / Tavily / SearXNG / DuckDuckGo
- **repomap** — token-budgeted file→symbol overview for LLM context (PageRank-scored)
- **ast-search** — pure-Go tree-sitter for Go, Python, JS/TS, Rust, Java, C, Ruby — structural code search and impact analysis
- **lsp** — auto-detected LSP servers per language — hover, definition, references, symbol search
- **bonsai-graph** — embeddable Dgraph (bonsai) for memory relations + code-knowledge mirror; DQL-queryable; Explorer UI mounted at /graph/ in `ycode serve`
- **git-tools** — 31 native go-git operations (branch, worktree, push, stash, log, …) — no shell-out required
- **gitea-server** — embedded Gitea git server — no external git host needed for agent workspaces
- **github-tools** — PR / issue / review / CI-checks via GitHub API — no `gh` binary, auth via GITHUB_TOKEN or ~/.config/gh
- **mcp-client** — full MCP client (stdio + SSE) for external tool servers; also exposes ycode tools via `ycode mcp serve`
- **container-runtime** — embedded podman for sandboxed bash execution and per-agent isolation (cgroups, network namespace, overlay FS)
- **ollama-runtime** — embedded Ollama inference runner for fully-local model execution (HuggingFace GGUF supported)
- **multi-provider** — Anthropic native + OpenAI-compatible covers OpenAI, xAI/Grok, DashScope/Qwen, Ollama, OpenRouter
- **serve-mode** — `ycode serve` exposes HTTP/WebSocket + NATS endpoints with embedded observability (Prometheus, Jaeger, VictoriaLogs, Perses)
- **observability** — OTEL traces + metrics + logs out of the box; agent-facing query tools (query_metrics, query_traces, query_logs)
- **agent-delegation** — recursive child agent spawning with depth limits; team, parallel, handoff, cron primitives
- **skills-system** — lifecycle-organised skill catalog: external (github.com/dhnt/dhnt/catalog) + internal (.agents/ycode/skills/) lanes; loader honours frontmatter executor (markdown / builtin / cnl)
- **plugin-system** — manifest-based plugins with hook lifecycle (PreToolUse, PostToolUse)
- **memory-system** — five-layer memory (working, episodic, compaction, procedural, persistent) with RRF-fused vector + Bleve + keyword + entity retrieval
- **session-management** — JSONL persistence, auto-compaction at 100K tokens, semantic summaries
- **permission-tiers** — three modes (ReadOnly, WorkspaceWrite, DangerFullAccess) with VFS-bounded path resolution and per-tool policy rules
- **self-healing** — automatic error recovery with classification and retry — no panic kills the loop
- **oauth-login** — PKCE OAuth flow for Claude authentication (`ycode login`)
- **embeddable-api** — `pkg/ycode/` exposes a public Go API for embedding the agent harness in other binaries
- **cross-platform** — single static Go binary; v0.1.0 ships linux/amd64 and darwin/arm64 (other platforms require code work — see release.yml matrix)
<!-- END FEATURES -->

## Prerequisites

- Go 1.26+
- One of:
  - `ANTHROPIC_API_KEY` for Anthropic models
  - `OPENAI_API_KEY` (+ optional `OPENAI_BASE_URL`) for OpenAI-compatible models

## Documentation

- [docs/strategy.md](./docs/strategy.md) -- **strategic roadmap, wedge positioning, feature-tier policy, operating principles** (read first for any planning or feature discussion)
- [docs/roadmap.md](./docs/roadmap.md) -- tactical feature-gap inventory (P0/P1/P2)
- [docs/leaderboards.md](./docs/leaderboards.md) -- benchmark targets and submission process
- [docs/release.md](./docs/release.md) -- release process, Homebrew tap bootstrap, troubleshooting
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

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

ycode is a pure Go CLI agent harness for autonomous software development. It provides 50+ tools, MCP/LSP integration, a plugin system, permission enforcement, multi-layered memory, and session management. It is a ground-up rewrite of priorart/clawcode (Claw Code), a Rust-based CLI agent harness. The reference implementation is at `priorart/clawcode/` (subdirectory in this repo).

## Build & Test

```bash
# Build binary with version injection
make build                        # outputs bin/ycode

# Or without make
go build -o bin/ycode ./cmd/ycode/

# Run all tests with race detector
make test                         # go test -race ./...

# Static analysis
make vet                          # go vet ./...

# Run a single test
go test -race -run TestFunctionName ./internal/path/to/package/

# Run tests for a specific package
go test -race ./internal/api/

# Cross-compile all platforms
make cross                        # linux/mac/win amd64+arm64 → dist/

# Full check (vet + test + build)
make all
```

## Architecture

The codebase follows a standard Go layout with `cmd/` for binaries, `internal/` for private packages, and `pkg/` for public API.

### Key runtime flow

1. **Entry**: `cmd/ycode/main.go` → cobra CLI → either interactive REPL (`internal/cli/app.go`) or one-shot mode
2. **Conversation loop**: `internal/runtime/conversation/runtime.go` assembles API requests, sends to provider, dispatches tool calls via `ToolExecutor`
3. **System prompt**: `internal/runtime/prompt/builder.go` assembles sections with a static/dynamic boundary for cache optimization. Static sections (role, rules) are above the boundary; dynamic sections (environment, git context, instruction files, memories) are below
4. **Tool dispatch**: `internal/tools/registry.go` maps tool names to handlers. Tools are either always-available (bash, file ops) or deferred (discovered via ToolSearch)
5. **Session**: `internal/runtime/session/` persists conversations as JSONL, with auto-compaction at 100K tokens producing semantic summaries

### Provider layer (`internal/api/`)

- `client.go` defines the `Provider` interface (Send, Kind)
- `anthropic.go` implements Anthropic API with SSE streaming
- `openai_compat.go` handles OpenAI-compatible providers (OpenAI, xAI, Ollama, etc.)
- `prompt_cache.go` fingerprints prompts for cache hit detection (TTL: 30s completion, 5min prompt)

### Memory system (`internal/runtime/memory/`)

Five layers: working (context window) → short-term (session JSONL) → long-term (compaction summaries) → contextual (CLAUDE.md ancestry) → persistent (file-based `~/.ycode/projects/`). Memory types: user, feedback, project, reference. MEMORY.md is an index kept under 200 lines.

### Config (`internal/runtime/config/`)

Three-tier merge: user (`~/.ycode/settings.json`) > project (`.ycode/settings.json`) > local (`.ycode/settings.local.json`).

### Permission (`internal/runtime/permission/`)

Three modes: ReadOnly, WorkspaceWrite, DangerFullAccess. Each tool declares its required permission level. The enforcer checks policy rules and prompts the user when needed.

## Dependencies

Only permissive licenses (MIT, Apache-2.0, BSD). Key deps:
- `github.com/spf13/cobra` -- CLI framework
- `github.com/charmbracelet/bubbletea` -- TUI/REPL
- `github.com/charmbracelet/glamour` -- Markdown rendering
- `github.com/alecthomas/chroma/v2` -- Syntax highlighting
- `github.com/google/uuid` -- UUIDs
- Go stdlib for everything else (Go 1.25+)

## Key Design Decisions

- **Map-based ToolRegistry** with runtime registration (plugins/MCP add tools without recompilation) instead of Rust's static match
- **`RuntimeContext` struct** holds all registries -- no global state
- **`context.Context` propagation** everywhere for cancellation/timeout
- **JSONL sessions** for interop with priorart/clawcode format
- **Section-based prompt assembly** with dynamic boundary marker for cache optimization
- **Per-tool middleware** for permission, logging, timing as composable wrappers
- **Recursive agent delegation** up to configurable depth (default: 3)

## Documentation

- [USAGE.md](./USAGE.md) -- CLI commands, configuration, sessions, tools, workflows
- [docs/plan.md](./docs/plan.md) -- full architecture, design decisions, tool catalog, memory/prompt diagrams
- [docs/todo.md](./docs/todo.md) -- implementation checklist (all phases complete)

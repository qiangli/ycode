# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

ycode — pure Go CLI agent harness for autonomous software development. Pure Go (go 1.26+), permissive-license dependencies only. Single static binary with embedded observability stack.

**Read [INSTRUCTIONS.md](./INSTRUCTIONS.md) for shared conventions** — skill dispatch rules, layered build system, testing guidelines, and commit conventions that apply to all AI agents.

## Build Commands

```bash
make build          # full quality gate: tidy → fmt → vet → compile → test → verify
make compile        # quick compile only (bin/ycode)
make test           # unit tests only (-short -race)
make deploy         # deploy to localhost:58080
make validate       # integration tests against running instance
make cross          # cross-compile all platforms (dist/)
make install        # build + install to ~/bin/
```

Single test: `go test -short -race -run TestName ./internal/path/to/package/`

Integration tests: `go test -tags integration -v -count=1 ./internal/integration/...`

The `PACKAGES` variable in the Makefile excludes `priorart/` from all Go commands.

## Architecture

### Runtime Flow

1. **Entry**: `cmd/ycode/main.go` → cobra CLI → interactive REPL (`internal/cli/app.go`) or one-shot mode
2. **Conversation loop**: `internal/runtime/conversation/runtime.go` — assembles API requests, sends to provider, dispatches tool calls via `ToolExecutor`, loops until no more tool calls
3. **System prompt**: `internal/runtime/prompt/builder.go` — section-based assembly with static/dynamic boundary for provider cache optimization
4. **Tool dispatch**: `internal/tools/registry.go` — map-based registry, always-available tools (bash, read/write/edit_file, glob/grep_search) vs deferred tools (discovered via ToolSearch)
5. **Provider layer**: `internal/api/` — `Provider` interface with Anthropic (`anthropic.go`) and OpenAI-compatible (`openai_compat.go`) implementations
6. **Session**: `internal/runtime/session/` — JSONL persistence, auto-compaction at 100K tokens

### Key Design Patterns

- **`RuntimeContext` struct** holds all registries — no global state
- **Per-tool middleware** for permission, logging, timing as composable wrappers
- **Runtime tool registration** — plugins and MCP add tools without recompilation
- **Three-tier config merge**: user (`~/.config/ycode/`) > project (`.agents/ycode/`) > local (`.agents/ycode/settings.local.json`)
- **Five-layer memory**: working (context) → short-term (session JSONL) → long-term (compaction) → contextual (CLAUDE.md ancestry) → persistent (file-based)
- **Three-layer build system**: Makefile (deps only) → scripts/ (bash orchestration) → Go (all logic)

### Observability

Fully embedded stack (OTEL Collector, Prometheus, Jaeger, VictoriaLogs, Perses) running as goroutines — no external processes. Components are in `internal/collector/` and `internal/observability/`. External submodules in `external/`.

## Testing

- **Unit tests**: `*_test.go` alongside source. Use `testing.Short()` for slow paths.
- **Integration tests**: `internal/integration/` with `//go:build integration` tag. Test against running services.
- **No test logic in bash.** Scripts invoke `go test` but contain no assertions.

## Claude Code-Specific Notes

### Committing Changes

When committing, also apply this Claude Code-specific behavior on top of the `/commit` skill:

- **Use the initial git status snapshot.** The system prompt includes the git status and diff captured at session start. Compare current `git status` against that snapshot to distinguish pre-existing dirty files from changes made during this session — do not stage pre-existing changes.

### Skills

When a user message starts with `/<name>`, read `skills/<name>/skill.md` and follow its instructions. List available skills with `ls skills/*/skill.md`.

## For More Detail

Read these on demand, not upfront:
- [USAGE.md](./USAGE.md) — CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) — full architecture, design decisions, component details

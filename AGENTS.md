# AGENTS.md

Instructions for AI coding agents working on this repository.

ycode -- pure Go CLI agent harness for autonomous software development. Go 1.26+, permissive-license dependencies only. Single static binary with embedded observability stack.

## Build Commands

```bash
make init           # REQUIRED first: initialize git submodules (observability stack)
make build          # full quality gate: tidy -> fmt -> vet -> compile -> test -> verify
make compile        # quick compile only (bin/ycode)
make test           # unit tests only (-short -race)
make deploy         # deploy to localhost:58080 (HOST=<remote> for remote)
make validate       # integration tests against running instance
make cross          # cross-compile all platforms (dist/)
make install        # build + install to ~/bin/
```

Single test: `go test -short -race -run TestName ./internal/path/to/package/`

Integration tests: `go test -tags integration -v -count=1 ./internal/integration/...`

`PACKAGES` in the Makefile excludes `priorart/` from all Go commands.

## Architecture

Entry: `cmd/ycode/main.go` -> cobra CLI -> REPL (`internal/cli/app.go`) or one-shot mode.

Core loop (`internal/runtime/conversation/runtime.go`): assemble request -> send to provider -> dispatch tool calls -> loop until done.

Key subsystems:
- **Providers** (`internal/api/`): Anthropic, OpenAI-compatible, Gemini
- **Tools** (`internal/tools/`): map-based registry, always-available vs deferred, per-tool middleware
- **Prompt** (`internal/runtime/prompt/`): section-based assembly with static/dynamic cache boundary
- **Session** (`internal/runtime/session/`): JSONL persistence, auto-compaction at 100K tokens
- **Storage** (`internal/storage/`): KV, SQLite, vector DB, full-text search
- **Observability** (`internal/collector/`, `internal/observability/`): embedded OTEL stack; submodules in `external/`
- **Plugins** (`internal/plugins/`): hook lifecycle, runtime tool registration
- **Self-heal** (`internal/selfheal/`): error classification and auto-recovery

Design: `RuntimeContext` (no global state), three-tier config merge, five-layer memory, three-layer build system (Makefile -> scripts/ -> Go).

## Conventions

See [INSTRUCTIONS.md](./INSTRUCTIONS.md) for full details: skill dispatch, build system layers, testing, commit rules.

**Layered build system** -- strict three-layer separation:
1. **Makefile** -- dependency graph only. No multi-line logic or embedded bash blocks.
2. **scripts/** -- bash orchestration only. Sequencing, env setup, conditionals.
3. **Go** -- all logic. Tests, utilities, and any non-trivial computation must be in Go.

**No test logic in bash.** Scripts may invoke `go test` but must not contain assertions, HTTP calls for validation, or result parsing.

**Commit conventions**: stage files by name (never `git add -A`), only stage your own changes, match the repo's prefix style from `git log` (`fix:`, `feat:`, `docs:`).

## Submodule Dependencies

The project uses local `replace` directives for embedded observability components:
- `external/victorialogs/` -> `github.com/VictoriaMetrics/VictoriaLogs`
- `external/jaeger/` -> `github.com/jaegertracing/jaeger`
- `external/perses/` -> `github.com/perses/perses`
- `external/memos/` -> `github.com/usememos/memos`

Run `make init` before first build to populate submodules.

## Skills

On disk: `build`, `claude`, `deploy`, `learn`, `setup`, `validate` (in `skills/`). Internal: `/init`, `/commit` (embedded in binary). See [INSTRUCTIONS.md](./INSTRUCTIONS.md) for dispatch rules.

## Agent-Specific Configuration

Each agent tool should store its private config under its own directory. The shared instructions live here in `AGENTS.md`; agent-specific overrides go in:

| Agent | Config location |
|-------|----------------|
| Claude Code | `.claude/` |
| ycode | `.agents/ycode/` |
| Cursor | `.cursor/` |
| Copilot | `.github/copilot-instructions.md` |
| OpenCode | `.opencode/` |

Agent-specific files should be minimal -- only settings, permissions, or behaviors that are unique to that agent. All project conventions belong in this file or in [INSTRUCTIONS.md](./INSTRUCTIONS.md).

## References

Read on demand:
- [INSTRUCTIONS.md](./INSTRUCTIONS.md) -- conventions, skill system, build/test/commit rules
- [USAGE.md](./USAGE.md) -- CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) -- full architecture, design decisions, component details

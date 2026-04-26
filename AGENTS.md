# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

ycode -- pure Go CLI agent harness for autonomous software development. Go 1.25+, permissive-license dependencies only.

## Build Commands

```bash
make init           # REQUIRED first: initialize git submodules + fetch Perses plugins
make build          # full quality gate: tidy -> fmt -> vet -> compile -> test -> verify
make compile        # quick compile only (bin/ycode)
make test           # unit tests only (-short -race)
make cross          # cross-compile all platforms (dist/)
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
- **Observability** (`internal/collector/`, `internal/observability/`): embedded OTEL stack; Perses plugins embedded via `go:embed`; submodules in `external/`
- **Plugins** (`internal/plugins/`): hook lifecycle, runtime tool registration

Design: `RuntimeContext` (no global state), three-tier config merge, five-layer memory.

## Skills

When the user's message starts with `/<name>` (e.g. `/build`, `/deploy`, `/learn`), read `skills/<name>/skill.md` and follow its instructions exactly. Everything after `/<name> ` is `ARGS` — pass it wherever the skill references `{{ARGS}}`. If no matching skill file exists, tell the user.

Project skills in `skills/`: `/build`, `/claude`, `/deploy`, `/learn`, `/setup`, `/validate`. Some skills (`/init`, `/commit`) are embedded in the ycode binary and dispatched via the `Skill` tool.

## Development Cycle: Build -> Deploy -> Validate

```bash
make build                              # full quality gate (must pass before deploy)
make deploy                             # localhost:58080 by default
make validate                           # integration/acceptance/perf tests against running instance

make deploy HOST=staging PORT=58080     # remote deploy (passwordless SSH required)
make validate HOST=staging PORT=58080   # validate remote
```

Each step depends on the previous one succeeding. On failure: diagnose, fix source, re-run from `make build`. Allow up to 3 fix-and-retry cycles before escalating.

## Conventions

See [INSTRUCTIONS.md](./INSTRUCTIONS.md) for full details: skill dispatch, build system layers, testing, commit rules.

**Layered build system** -- strict three-layer separation:
1. **Makefile** -- dependency graph only. No multi-line logic or embedded bash blocks.
2. **scripts/** -- bash orchestration only. Sequencing, env setup, conditionals.
3. **Go** -- all logic. Tests, utilities, and any non-trivial computation must be in Go.

**No test logic in bash.** Scripts may invoke `go test` but must not contain assertions, HTTP calls for validation, or result parsing.

**Commit conventions**: stage files by name (never `git add -A`), only stage your own changes, match the repo's prefix style from `git log` (`fix:`, `feat:`, `docs:`).

**Pre-commit checks** -- ALWAYS run before committing:
```bash
go fmt ./...              # fix formatting
go vet ./...              # catch issues
go mod tidy               # sync dependencies
make compile              # ensure it builds
```
All four must pass with no errors. Do NOT commit code with formatting issues, vet warnings, or stale go.mod/go.sum.

## Directory Boundaries

- **`priorart/`** -- **read-only reference code.** Never modify files under `priorart/`. These are upstream submodules kept for exploration, research, and design reference. Do not create, edit, or delete anything in this tree.
- **`external/`** -- vendored dependencies used by the ycode build. If code from `priorart/` (or any external project) needs to be incorporated into ycode, vendor it into `external/` with appropriate attribution.

## Submodule Dependencies

The project uses local `replace` directives for embedded observability components:
- `external/victorialogs/` -> `github.com/VictoriaMetrics/VictoriaLogs`
- `external/jaeger/` -> `github.com/jaegertracing/jaeger`
- `external/perses/` -> `github.com/perses/perses`
- `external/memos/` -> `github.com/usememos/memos`

Run `make init` before first build to populate submodules and fetch Perses plugin archives.

## Build Notes

macOS arm64: binaries are ad-hoc codesigned after compile (`codesign -f -s -`). Copying the binary (e.g. `cp`) invalidates the signature — re-sign after install. The `make install` target handles this automatically.

## References

Read on demand:
- [INSTRUCTIONS.md](./INSTRUCTIONS.md) -- conventions, skill system, build/test/commit rules
- [USAGE.md](./USAGE.md) -- CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) -- full architecture, design decisions, component details
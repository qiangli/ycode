# AGENTS.md

This file provides guidance to AI coding assistants working on this repository.
It is tool-agnostic — it applies to Claude Code, OpenCode, Codex, ycode, and any other AI agent.
CLAUDE.md is a symlink to this file.

ycode -- pure Go CLI agent harness for autonomous software development. Go 1.26+, permissive-license dependencies only.

## First-Time Setup

```bash
make init                              # REQUIRED: initialize git submodules + fetch Perses plugins
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY for OpenAI-compatible providers
```

## Build Commands

```bash
make build          # full quality gate: tidy -> fmt -> vet -> compile -> test -> verify
make compile        # quick compile only (bin/ycode)
make install        # build + install to ~/bin/ycode (re-signs on macOS)
make test           # unit tests only (-short -race)
make cross          # cross-compile all platforms (dist/)
```

Single test: `go test -short -race -run TestName ./internal/path/to/package/`

Integration tests: `go test -tags integration -v -count=1 ./internal/integration/...`

Additional test targets:
```bash
make test-container   # container integration tests (requires podman)
make test-gitserver   # git server workspace tests
make test-tui         # TUI integration tests (direct Update + teatest)
make test-tui-e2e     # TUI E2E tests in a PTY (requires compiled binary)
make test-tui-fuzz    # TUI fuzz tests (30s each)
make test-ui          # Playwright browser tests (e2e/ dir, requires running server + npx)
make test-all         # all of the above combined
```

Validation against a running instance:
```bash
make validate         # Go integration tests against running server
make validate-ui      # Playwright browser tests against running server
make validate-all     # both
```

Inference runner (local Ollama):
```bash
make runner-download  # download pre-built Ollama runner for current platform
make runner-build     # build runner from source (requires C++ toolchain)
make runner-check     # verify runner binary + health check
```

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
- **Server** (`internal/server/`, `internal/service/`): HTTP/WebSocket/NATS serve mode (`ycode serve`)
- **Web** (`internal/web/`): embedded web UI (minimal)
- **Inference** (`internal/inference/`): local model inference via embedded Ollama runner
- **Container** (`internal/container/`): container management (Podman)
- **Git server** (`internal/gitserver/`): git server workspace operations
- **Memos** (`internal/memos/`): memos integration for note-taking
- **Event bus** (`internal/bus/`): internal event routing between subsystems

Public embedding API: `pkg/ycode/` exposes `NewAgent`, `Run`, and functional options for embedding ycode as a library in other Go programs.

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

See [docs/instructions.md](./docs/instructions.md) for full details: skill dispatch, build system layers, testing, commit rules.

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
All four must pass with no errors. Do NOT commit code with formatting issues, vet warnings, or stale go.mod/go.sum. Alternatively, `make build` runs all of these (plus tests) as a single quality gate.

## Directory Boundaries

- **`priorart/`** -- **read-only reference code.** Never modify files under `priorart/`. These are upstream submodules kept for exploration, research, and design reference. Do not create, edit, or delete anything in this tree. The `PACKAGES` variable in the Makefile excludes `priorart/` from all Go commands.
- **`external/`** -- vendored dependencies used by the ycode build. If code from `priorart/` (or any external project) needs to be incorporated into ycode, vendor it into `external/` with appropriate attribution.

## Submodule Dependencies

The project uses local `replace` directives for embedded observability components:
- `external/victorialogs/` -> `github.com/VictoriaMetrics/VictoriaLogs`
- `external/jaeger/` -> `github.com/jaegertracing/jaeger`
- `external/perses/` -> `github.com/perses/perses`
- `external/memos/` -> `github.com/usememos/memos`

## Build Notes

macOS arm64: binaries are ad-hoc codesigned after compile (`codesign -f -s -`). Copying the binary (e.g. `cp`) invalidates the signature — re-sign after install. The `make install` target handles this automatically.

## References

Read on demand:
- [docs/instructions.md](./docs/instructions.md) -- conventions, skill system, build/test/commit rules
- [docs/usage.md](./docs/usage.md) -- CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) -- full architecture, design decisions, component details
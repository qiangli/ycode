# AGENTS.md

Instructions for AI coding agents working on this repository.

ycode -- pure Go CLI agent harness for autonomous software development. Go 1.26+, permissive-license dependencies only. Single static binary with embedded observability stack.

**Read [INSTRUCTIONS.md](./INSTRUCTIONS.md)** -- shared conventions: skill dispatch, build system layers, testing, commit rules.

## Build Commands

```bash
make build          # full quality gate: tidy -> fmt -> vet -> compile -> test -> verify
make compile        # quick compile only (bin/ycode)
make test           # unit tests only (-short -race)
make deploy         # deploy to localhost:58080 (HOST=<remote> for remote)
make validate       # integration tests against running instance
make cross          # cross-compile all platforms (dist/)
make install        # build + install to ~/bin/
make init           # initialize git submodules (required for observability stack)
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

## Skills

On disk: `build`, `claude`, `deploy`, `learn`, `setup`, `validate` (in `skills/`). Internal: `/init`, `/commit` (embedded in binary). See [INSTRUCTIONS.md](./INSTRUCTIONS.md) for dispatch rules.

## References

Read on demand:
- [INSTRUCTIONS.md](./INSTRUCTIONS.md) -- conventions, skill system, build/test/commit rules
- [USAGE.md](./USAGE.md) -- CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) -- full architecture, design decisions, component details

# AGENTS.md

This file provides guidance to AI coding assistants working on this repository.
It is tool-agnostic — it applies to Claude Code, OpenCode, Codex, ycode, and any other AI agent.
CLAUDE.md is a symlink to this file.

ycode -- pure Go CLI agent harness for autonomous software development. Go 1.26+, permissive-license dependencies only.

## First-Time Setup

```bash
make init                              # REQUIRED: initialize submodules, fetch Perses plugins, gzip embedded assets
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY for OpenAI-compatible providers
```

## Build Commands

```bash
make build          # full quality gate: tidy -> fmt -> vet -> compile -> test -> verify
make compile        # quick compile only (bin/ycode)
make compile-debug  # compile with debug symbols (for profiling/debugging)
make install        # build + install to ~/bin/ycode (re-signs on macOS)
make test           # unit tests only (-short -race)
make cross          # cross-compile all platforms (dist/)
```

Single test and integration test:
```bash
go test -short -race -run TestName ./internal/path/to/package/
go test -tags integration -v -count=1 ./internal/integration/...
```

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

Entry: `cmd/ycode/main.go` -> cobra CLI -> REPL (`internal/cli/app.go`) or one-shot mode. Core loop in `internal/runtime/conversation/runtime.go`: assemble request -> send to provider -> dispatch tool calls -> loop until done. Public embedding API: `pkg/ycode/`. Design: `RuntimeContext` (no global state), three-tier config merge, five-layer memory.

## Skills

When the user's message starts with `/<name>`, read `skills/<name>/skill.md` and follow it. Everything after `/<name> ` is `ARGS`. Project skills: `/build`, `/claude`, `/deploy`, `/learn`, `/setup`, `/validate`, `/bench-instructions`. Some skills (`/init`, `/commit`) are embedded in the binary.

## Development Cycle: Build -> Deploy -> Validate

```bash
make build                              # full quality gate (must pass before deploy)
make deploy                             # localhost:58080 by default
make validate                           # integration/acceptance/perf tests against running instance

make deploy HOST=staging PORT=58080     # remote deploy (passwordless SSH required)
make validate HOST=staging PORT=58080   # validate remote
```

Each step depends on the previous one succeeding. On failure: diagnose, fix source, re-run from `make build`.

## Conventions

**Layered build system** -- strict three-layer separation. Do not put logic in the Makefile (dependency graph only). Do not put logic in scripts/ (orchestration only). All logic must be in Go.

**No test logic in bash.** Scripts may invoke `go test` but must not contain assertions, HTTP calls for validation, or result parsing.

**Dependencies** -- never add a dependency with a non-permissive license (GPL, AGPL, SSPL, CPAL). Only MIT, Apache-2.0, BSD, ISC, and MPL-2.0 are allowed.

**No global state** -- never use package-level `var` for mutable state or registries. All state belongs on `RuntimeContext` or function parameters.

**Logging discipline** -- do not add `log.Printf` or `fmt.Println` for debugging. Always use the structured logger from `RuntimeContext`. Never leave debug output on stderr — noisy shutdown logs have been a repeated source of fixes.

**Test isolation** -- always use `t.TempDir()` for test files, never write to the working directory. Always use `testing.Short()` to skip slow tests. Do not add `//go:build integration` tags to unit tests.

**Commit conventions**: stage files by name (never `git add -A` or `git add .`). Only stage your own changes — do not stage pre-existing modifications. Match the repo's prefix style from `git log` (`fix:`, `feat:`, `docs:`).

**Pre-commit checks** -- ALWAYS run `make build` before committing. It runs tidy, fmt, vet, compile, and test in the correct order with `priorart/` excluded. If you need to run steps manually:
```bash
PACKAGES=$(go list ./... | grep -v '/priorart/')
go fmt $PACKAGES          # fix formatting
go vet $PACKAGES          # catch issues
go mod tidy               # sync dependencies
make compile              # ensure it builds
```
Never use bare `./...` — it hits read-only `priorart/` packages. All steps must pass with no errors.

## Directory Boundaries

- **`priorart/`** -- **read-only.** Never modify, create, or delete anything under `priorart/`. Use `$(go list ./... | grep -v '/priorart/')` instead of `./...` for manual Go commands.
- **`external/`** -- vendored submodules for the ycode build. Do not modify directly; vendor new code with attribution.

## Evaluation

```bash
make eval-agentsmd                     # validate AGENTS.md quality (static, no LLM)
make eval-contract                     # contract-tier evals (no LLM, deterministic)
```

## References

Read on demand:
- [docs/instructions.md](./docs/instructions.md) -- conventions, skill system, build/test/commit rules
- [docs/usage.md](./docs/usage.md) -- CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) -- full architecture, design decisions, component details
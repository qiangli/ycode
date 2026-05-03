# AGENTS.md

This file provides guidance to AI coding assistants working in this repository.

ycode — pure Go CLI agent harness for autonomous software development. Go 1.26+, permissive-license dependencies only. CLAUDE.md is a symlink to this file.

## First-Time Setup

```bash
make init                              # REQUIRED: initialize submodules, fetch Perses plugins, gzip embedded assets
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY for OpenAI-compatible providers
```

## Build Commands

```bash
make build          # full quality gate: tidy → fmt → vet → compile → test → verify
make compile        # quick compile only (bin/ycode)
make compile-full   # single binary with embedded podman + runner
make compile-debug  # compile with debug symbols (for profiling/debugging)
make install        # build + install to ~/bin/ycode (re-signs on macOS)
make test           # unit tests only (-short -race)
make cross          # cross-compile all platforms (dist/)
```

Manual `go build` requires build tags (handled automatically by `make compile`):
```bash
go build -tags "sqlite,sqlite_unlock_notify,bindata" -o bin/ycode ./cmd/ycode/
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

Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot mode. Core loop in `internal/runtime/conversation/runtime.go`: assemble request → send to provider → dispatch tool calls → loop until done. Public embedding API: `pkg/ycode/`. Design: `RuntimeContext` (no global state), four-tier config merge, five-layer memory.

**Key entry points:**
- Provider layer: `internal/api/` — `Provider` interface (`Send(ctx, *Request) → stream`). Anthropic native + OpenAI-compatible (covers OpenAI, xAI, Gemini, Ollama). Model aliases resolved in `api/provider.go`.
- Tool system: `internal/tools/registry.go` — `Registry` maps tool names to `ToolSpec` handlers. Tools are either always-available (sent every request: bash, file ops, search) or deferred (discovered via `ToolSearch`, activated with TTL=8 turns). New tools: add a `RegisterXxxHandlers(r *Registry)` function.
- Prompt assembly: `internal/runtime/prompt/builder.go` — static sections (cacheable) above a dynamic boundary (environment, git, instructions, memories).
- Config: `internal/runtime/config/config.go` — merges four files in order: `~/.config/ycode/settings.json` (user) → `<project>/.agents/ycode/settings.json` → `<cwd>/.agents/ycode/settings.json` (local) → `settings.local.json` (gitignored). `Instructions` and `AllowedDirectories` append; all other fields override.
- Permission modes: ReadOnly (read/search only) → WorkspaceWrite (file modifications within VFS boundaries) → DangerFullAccess (shell, process control, MCP). Each tool declares its required mode in `ToolSpec.RequiredMode`.
- AST search: `internal/runtime/treesitter/` — pure Go tree-sitter (gotreesitter) for Go, Python, JS/TS, Rust, Java, C, Ruby. Structural code search, symbol extraction, impact analysis. No CGO required. Container fallback via `internal/runtime/astgrep/` for rewrite operations only.
- MCP: `internal/runtime/mcp/` — full MCP client (stdio + SSE transports) for connecting to external tool servers, and MCP server mode (`ycode mcp serve`) to expose ycode tools. Config: `~/.config/ycode/mcp.json` or `.agents/ycode/mcp.json`.
- GitHub: `internal/runtime/github/` — PR create/list/review/comment, issue list/get/comment, CI check status. Auth: `GITHUB_TOKEN` → `GH_TOKEN` → `~/.config/gh/hosts.yml` (no external `gh` binary). Tools registered as deferred (via ToolSearch).
- Git: `internal/runtime/toolexec/` — 31 native go-git NativeFuncs (3-tier: native → host exec → container). Shell: `internal/runtime/bash/` — in-process mvdan/sh interpreter with security ExecHandler middleware (Setpgid, pre-exec validation).
- Container tools (browser automation, sandbox): require podman. Managed in `internal/container/`.
- Embedded services: Gitea git server (`internal/gitserver/`), Ollama inference runner (`internal/inference/`), SearXNG search (`internal/runtime/searxng/`). Started by `ycode serve`.
- Repo map: `internal/runtime/repomap/` — generates token-budgeted file→symbol overview for LLM context. PageRank scoring with Aider-inspired heuristics. Injected into system prompt.
- Memory: five layers — working (context window) → episodic (JSONL sessions) → compaction (LLM summaries) → procedural (AGENTS.md discovery) → persistent (markdown files with YAML frontmatter). 7 types × 4 scopes. Retrieval uses RRF fusion across 4 backends (vector, Bleve, keyword, entity) + MMR diversity re-ranking. Dynamic value scoring with reward backpropagation. Entity extraction and linking. Structured user profile. Turn-time memory injection. Temporal validity windows. Background dreaming with similarity-based consolidation. Managed in `internal/runtime/memory/`, persisted via `internal/storage/` (SQLite, Bleve FTS, vector).

## Skills

When the user's message starts with `/<name>`, read `skills/<name>/skill.md` and follow it. Everything after `/<name> ` is `ARGS`. Project skills: `/audit`, `/autopilot`, `/build`, `/claude`, `/deploy`, `/eval`, `/learn`, `/oci`, `/setup`, `/validate`, `/bench-instructions`. Some skills (`/init`, `/commit`) are embedded in the binary.

## Development Cycle: Build → Deploy → Validate

```bash
make build                              # full quality gate (must pass before deploy)
bin/ycode serve                         # start local server (Gitea, Ollama, SearXNG on :58080)
make deploy                             # deploy to localhost:58080 (or remote with HOST=)
make validate                           # integration/acceptance/perf tests against running instance

make deploy HOST=staging PORT=58080     # remote deploy (passwordless SSH required)
make validate HOST=staging PORT=58080   # validate remote
```

Each step depends on the previous one succeeding. On failure: diagnose, fix source, re-run from `make build`.

## Conventions

**Layered build system** — strict three-layer separation. Do not put logic in the Makefile (dependency graph only). Do not put logic in scripts/ (orchestration only). All logic must be in Go.

**No test logic in bash.** Scripts may invoke `go test` but must not contain assertions, HTTP calls for validation, or result parsing.

**Dependencies** — never add a dependency with a non-permissive license (GPL, AGPL, SSPL, CPAL). Only MIT, Apache-2.0, BSD, ISC, and MPL-2.0 are allowed.

**No global state** — never use package-level `var` for mutable state or registries. All state belongs on `RuntimeContext` or function parameters.

**Logging discipline** — do not add `log.Printf` or `fmt.Println` for debugging. Always use the structured logger from `RuntimeContext`. Never leave debug output on stderr — noisy shutdown logs have been a repeated source of fixes.

**Test isolation** — always use `t.TempDir()` for test files, never write to the working directory. Always use `testing.Short()` to skip slow tests. Do not add `//go:build integration` tags to unit tests.

**Commit conventions**: stage files by name (never `git add -A` or `git add .`). Only stage your own changes — do not stage pre-existing modifications. Match the repo's prefix style from `git log` (`fix:`, `feat:`, `docs:`).

**Pre-commit checks** — ALWAYS run `make build` before committing. It runs tidy, fmt, vet, compile, and test in the correct order with `priorart/` excluded. If you need to run steps manually:
```bash
PACKAGES=$(go list ./... | grep -v '/priorart/')
go fmt $PACKAGES          # fix formatting
go vet $PACKAGES          # catch issues
go mod tidy               # sync dependencies
make compile              # ensure it builds
```
Never use bare `./...` — it hits read-only `priorart/` packages. All steps must pass with no errors.

## Directory Boundaries

- **`priorart/`** — **read-only.** Never modify, create, or delete anything under `priorart/`. Use `$(go list ./... | grep -v '/priorart/')` instead of `./...` for manual Go commands.
- **`external/`** — vendored submodules for the ycode build. Do not modify directly; vendor new code with attribution.

## Evaluation

```bash
make eval-agentsmd                     # validate AGENTS.md quality (static, no LLM)
make eval-contract                     # contract-tier evals (no LLM, deterministic)
make eval-smoke                        # smoke-tier evals (real LLM, requires provider)
make eval-behavioral                   # behavioral evals (trajectory analysis)
make eval-e2e                          # E2E evals (full coding tasks)
make eval-all-evals                    # all tiers combined
```

## References

Read on demand:
- [docs/instructions.md](./docs/instructions.md) — conventions, skill system, build/test/commit rules
- [docs/usage.md](./docs/usage.md) — CLI modes, config, tools, workflows
- [docs/architecture.md](./docs/architecture.md) — full architecture, design decisions, component details
- [docs/memory.md](./docs/memory.md) — five-layer memory system, search backends, temporal decay
- [docs/swarm.md](./docs/swarm.md) — agent orchestration, YAML definitions, handoff flows
- [docs/persistence.md](./docs/persistence.md) — storage technology analysis and design decisions
- [docs/autonomous-loop.md](./docs/autonomous-loop.md) — RESEARCH→PLAN→BUILD→EVALUATE→LEARN loop implementation

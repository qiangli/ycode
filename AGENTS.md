# AGENTS.md

This file provides guidance to AI coding assistants working in this repository.

ycode — pure Go CLI agent harness for autonomous software development. Go 1.26+, permissive-license dependencies only. CLAUDE.md is a symlink to this file.

> **Read first:** [`docs/strategy.md`](./docs/strategy.md) — the wedge, feature-tier build-tag policy, roadmap, and operating principles. Before suggesting features, refactors, or architectural changes, consult the strategy doc to confirm alignment with the current wedge ("local-first, single-binary, runs offline") and graduation criteria. New features land behind the `experimental` build tag by default and graduate to `stable` only after meeting the criteria documented there.

## Quick Orientation

ycode is a single Go binary that runs an LLM-driven coding agent locally. The conversation loop lives in `internal/runtime/conversation/runtime.go` (assemble request → send to provider → dispatch tool calls → repeat). Entry is `cmd/ycode/main.go`; the public embedding API is `pkg/ycode/`. State flows through `RuntimeContext` — there is no global mutable state. When in doubt about scope or architecture, read `docs/strategy.md` first.

## First-Time Setup

```bash
make init                              # REQUIRED: initialize submodules, fetch Perses plugins, gzip embedded assets
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY for OpenAI-compatible providers
make install-hooks                     # recommended: symlinks pre-push hook so `make ci` runs before every push
```

## Build Commands

```bash
make build          # full quality gate: tidy → fmt → vet → compile → test → verify
make compile        # quick compile only (bin/ycode)
make compile-full   # single binary with embedded podman + runner (much larger output; for offline distribution)
make compile-debug  # compile with debug symbols (for profiling/debugging)
make install        # build + install to ~/bin/ycode (re-signs on macOS)
make test           # unit tests only (-short -race)
make cross          # cross-compile all platforms (dist/)
```

Manual `go build` requires build tags (handled automatically by `make compile`):
```bash
go build -tags "sqlite,sqlite_unlock_notify,bindata" -o bin/ycode ./cmd/ycode/
```

Single test and integration test (use the first for fast unit iteration; the second when the test needs container/network setup):
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

## Running

```bash
bin/ycode                              # interactive REPL (auto-spawns a server if --connect not given)
bin/ycode prompt "explain the runtime" # one-shot; add --print for plain text (no markdown)
bin/ycode --model claude-sonnet-4-6    # override the configured model for this session
bin/ycode --connect ws://host:58080    # attach to an already-running server
bin/ycode serve                        # run the server explicitly (Gitea, Ollama, SearXNG, NATS, observability)
```

When the TUI auto-spawns a server, its stdout/stderr go to `~/.agents/ycode/observability/serve.log` — check there if the client connects but the server appears silent.

The model and provider come from settings.json (`model` field, schema in `internal/runtime/config/config.go`); `--model` overrides per-invocation. `bin/ycode model` manages the local Ollama registry.

## Architecture

Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot mode. Core loop in `internal/runtime/conversation/runtime.go`: assemble request → send to provider → dispatch tool calls → loop until done. Public embedding API: `pkg/ycode/`. Design: `RuntimeContext` (no global state), four-tier config merge, five-layer memory.

### Request lifecycle
- **Provider layer** (`internal/api/`) — `Provider` interface (`Send(ctx, *Request) → stream`). Anthropic native + OpenAI-compatible (covers OpenAI, xAI, Gemini, Ollama). Model aliases resolved in `api/provider.go`.
- **Tool system** (`internal/tools/registry.go`) — `Registry` maps tool names to `ToolSpec` handlers. Tools are either always-available (sent every request: bash, file ops, search) or deferred (discovered via `ToolSearch`, activated with TTL=8 turns). New tools: add a `RegisterXxxHandlers(r *Registry)` function.
- **Prompt assembly** (`internal/runtime/prompt/builder.go`) — static sections (cacheable) above a dynamic boundary (environment, git, instructions, memories).
- **Config** (`internal/runtime/config/config.go`) — merges four files in order: `~/.config/ycode/settings.json` (user) → `<project>/.agents/ycode/settings.json` → `<cwd>/.agents/ycode/settings.json` (local) → `settings.local.json` (gitignored). `Instructions` and `AllowedDirectories` append; all other fields override.
- **Permission modes** — ReadOnly (read/search only) → WorkspaceWrite (file modifications within VFS boundaries) → DangerFullAccess (shell, process control, MCP). Each tool declares its required mode in `ToolSpec.RequiredMode`. The `Registry`'s `PermissionPrompter` is interface-shaped — direct mode installs a TUI prompter; `ycode serve` installs a remote prompter (`internal/service/permission.go`) that publishes `permission.request` over the bus and blocks on the connected client's response. Do not install an in-process prompter on the server-side `App` — it must be the remote one so web/IDE clients see the request.
- **Client/server topology** (`cmd/ycode/autoserve.go`) — auto-spawns a server when no client URL is given; the TUI then connects via `WSClient`. Slash commands in this mode are not dispatched locally — the TUI sends the raw `/<name> <args>` text via `SendMessage`, the server's `LocalService.SendMessage` detects the leading `/` and routes to `executeCommandFromMessage`, which streams progress back as `EventCommandProgress`/`EventCommandDelta`/`EventCommandComplete` bus events. Direct mode (`m.cl == nil`) dispatches via the local `commands.Registry`.

### Tools & capabilities
- **AST search** (`internal/runtime/treesitter/`) — pure Go tree-sitter (gotreesitter) for Go, Python, JS/TS, Rust, Java, C, Ruby. Structural code search, symbol extraction, impact analysis. No CGO required. Container fallback via `internal/runtime/astgrep/` for rewrite operations only.
- **MCP** (`internal/runtime/mcp/`) — full MCP client (stdio + SSE transports) for connecting to external tool servers, and MCP server mode (`ycode mcp serve`) to expose ycode tools. Config: `~/.config/ycode/mcp.json` or `.agents/ycode/mcp.json`.
- **GitHub** (`internal/runtime/github/`) — PR create/list/review/comment, issue list/get/comment, CI check status. Auth: `GITHUB_TOKEN` → `GH_TOKEN` → `~/.config/gh/hosts.yml` (no external `gh` binary). Tools registered as deferred (via ToolSearch).
- **Git & shell** — `internal/runtime/toolexec/` exposes 31 native go-git NativeFuncs (3-tier: native → host exec → container). `internal/runtime/bash/` runs an in-process mvdan/sh interpreter with security ExecHandler middleware (Setpgid, pre-exec validation).
- **Container tools** (`internal/container/`) — browser automation and sandbox tools that require podman.
- **Repo map** (`internal/runtime/repomap/`) — generates token-budgeted file→symbol overview for LLM context. PageRank scoring with Aider-inspired heuristics. Injected into system prompt.

### Embedded services
Started by `ycode serve`: Gitea git server (`internal/gitserver/`), Ollama inference runner (`internal/inference/`), SearXNG search (`internal/runtime/searxng/`).

### Memory & state
Five layers — working (context window) → episodic (JSONL sessions) → compaction (LLM summaries) → procedural (AGENTS.md discovery) → persistent (markdown files with YAML frontmatter). 7 types × 4 scopes. Retrieval uses RRF fusion across 4 backends (vector, Bleve, keyword, entity) + MMR diversity re-ranking. Dynamic value scoring with reward backpropagation. Entity extraction and linking. Structured user profile. Turn-time memory injection. Temporal validity windows. Background dreaming with similarity-based consolidation. Managed in `pkg/memex/memory/`, persisted via `pkg/memex/store/` (SQLite, Bleve FTS, vector). Relations between memories and a mirror of the gfy code-knowledge graph live in `pkg/memex/graph/` (bonsai-backed, DQL queryable; Explorer UI mounted at `/graph/` in `ycode serve`; agent tool `query_graph_dql`).

## Skills

When the user's message starts with `/<name>`, read `skills/<name>/skill.md` and follow it. Everything after `/<name> ` is `ARGS`. See `skills/` for the project-skill catalog; `/init` and `/commit` are embedded in the binary.

## Self-Bootstrap (Foreman role)

**You are the Foreman.** Any agent reading this file with source-tree access is acting as the Foreman for this session. The Foreman holds full privileges (source tree, `docs/backlog/`, `ycode backlog`/`ycode foreman` CLIs, full Gitea admin token, all MCP tools). **Workers** are sandboxed subprocesses spawned via `/foreman` — they receive only one Gitea issue and one Loom workspace and do not read this file. The **Boss** (human user) talks to the Foreman either in chat or via `ycode foreman <verb>`. See `docs/backlog.md` for the full chain-of-command and privilege boundaries.

**Planning behavior.** When helping the user plan or brainstorm, write `docs/backlog/<slug>.md` files (one per task) with frontmatter `priority: p1|p2|p3`, `state: open`, `title`, optional `acceptance: [...]`. The reconciler (running inside `ycode serve`) syncs them to Gitea automatically every 60s. **`docs/backlog/` is the source of truth — Gitea is a derived coordination cache that can be wiped and rebuilt from the markdown.**

**Working behavior.** If you start a session with no specific user task, run `/foreman` (or for non-ycode agents that don't have the skill loaded: `ycode backlog list --priority p1` then dispatch a Worker via `ycode autopilot worker --issue N --loom-id ID` after `mcp__ycode-loom__loom_lease`). The reconciler / queue / orchestrator primitives live in `internal/gitserver/{backlog,queue,collab}/`; the loop body is `.agents/ycode/skills/ycode-foreman/skill.md`. Foreign agents discover this protocol via the `## Foreign agents (lighthouse pattern)` section below.

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

**Feature tiers** — new features land behind the `experimental` build tag and graduate to `stable` only after meeting the criteria in [`docs/strategy.md`](./docs/strategy.md). Check the strategy doc before proposing scope/architecture changes; the wedge ("local-first, single-binary, runs offline") is load-bearing.

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

**Pre-push CI parity** — `make ci` runs the exact same commands GitHub Actions does, in the same `ycode-builder` Docker image, with the same CGO system deps. Run it before push when you've touched anything CGO-adjacent (podman/storage, sqlite, gpgme), workflow files, or `go.work`. Slow (~5–10 min cold; ~2 min after the image cache warms). For pre-push automation, `make install-hooks` symlinks `scripts/git-hooks/pre-push` so every push runs `make ci` first; bypass with `git push --no-verify` or `YCODE_SKIP_CI_HOOK=1 git push`.

## Directory Boundaries

- **`priorart/`** — **read-only.** Never modify, create, or delete anything under `priorart/`. Use `$(go list ./... | grep -v '/priorart/')` instead of `./...` for manual Go commands.
- **`external/`** — vendored submodules for the ycode build. Do not modify directly; vendor new code with attribution.
- **`peers/`** — peer Go modules wired into `go.work` (e.g. `peers/bonsai`, the embedded graph database backing `pkg/memex/graph/`). Modules here are owned by this project and editable, but they are independent `go.mod`s — run `go mod tidy` inside the peer directory, not at the repo root.

## Foreign agents (lighthouse pattern)

ycode exposes its capabilities to *any* coding agent (Claude Code, Codex, Cursor, Continue, an older ycode build) via MCP, so agents in this tree can use ycode's AST search, sandbox, local Ollama, isolated Gitea workspaces, etc. without plugins or shell exec.

- `.mcp.json` at the repo root — committed lighthouse beam: any Claude Code session opened in this tree auto-registers `ycode mcp serve`. No manual MCP config needed.
- `~/.agents/ycode/manifest.json` — written by `ycode serve` listing every live endpoint (MCP routes, OTLP, NATS, Gitea, graph, ...). User-home global, so foreign agents in any codebase find it.
- `bin/ycode mcp serve` — stdio MCP server. Phase 0 ships infra only (empty `tools/list`); capability families plug into `mcp.NewCompositeHandler` per the recipe in [docs/lighthouse.md](./docs/lighthouse.md).

Adding a capability is one new `mcpserver.go` per family. See [docs/lighthouse.md](./docs/lighthouse.md) for the template and the federation discipline (ycode is the hub of *your* matrix, never the central hub).

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
- [docs/lighthouse.md](./docs/lighthouse.md) — exposing ycode capabilities to foreign coding agents via MCP; matrix + federation
- [docs/lighthouse-roadmap.md](./docs/lighthouse-roadmap.md) — what shipped (Phase 0), what's queued (Phase 1+), strategic anchors; pointer to canonical plan file

<!-- BEGIN YCODE -->
## ycode

This repo expects [ycode](https://github.com/qiangli/ycode) running locally as
agentic infrastructure. When acting as an agentic coding tool, see
[`.agents/ycode/AGENTS.md`](.agents/ycode/AGENTS.md) for capability descriptions and when to
prefer them. Run `ycode init --refresh` to update this section.
<!-- END YCODE -->

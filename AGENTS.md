# AGENTS.md

Guidance for AI coding assistants working in this repository.

## Project Overview

ycode — pure Go CLI agent harness. Single static binary, Go 1.26+, permissive-license dependencies only.

Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot.  
Core loop: `internal/runtime/conversation/runtime.go` — assemble request → provider → dispatch tool calls → repeat.

## First-Time Setup

```bash
 make init                              # REQUIRED: submodules and Gitea bindata/assets
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY
make install-hooks                     # pre-push hook runs `make ci`
```

Skipping `make init` leaves Gitea bindata/assets missing; tests and `ycode serve` fail in subtle ways.

## Build & Test

```bash
make build           # full gate: tidy → fmt → vet → compile → test → verify
make compile         # quick compile only
make test            # unit tests (-short -race)
make ci              # full GitHub Actions matrix in Docker (slow, definitive)
```

**Build tags** (see `Makefile`):
- Default: `sqlite,sqlite_unlock_notify,bindata`
- Auto-added when `.gz` exists: `embed_spawn`
- Manual: `go build -tags "sqlite,sqlite_unlock_notify,bindata" -o bin/ycode ./cmd/ycode/`

**Test patterns**:
```bash
# Single package / test
go test -short -race -run TestName ./internal/path/to/package/

# Never use bare `./...` — always exclude priorart/:
PACKAGES=$(go list ./... | grep -v '/priorart/')
```

**Specialized test targets** (read Makefile comments for prerequisites):
- `make test-gitserver` — embedded Gitea, ~4 min
- `make test-tui` / `make test-tui-e2e` — TUI lifecycle; e2e needs compiled binary + PTY
- `make test-ui` — Playwright (`cd e2e && npx playwright test`) against running server
- `make eval-{contract,smoke,behavioral,e2e}` — eval tiers; smoke/behavioral/e2e need live LLM provider

## Critical Conventions

**Directory boundaries:**
- `priorart/` — **NEVER modify.** Read-only reference implementations.
- `external/` — vendored submodules. Do not modify directly; bump submodule SHA instead.
- `peers/` — peer Go modules with own `go.mod`. Run `go mod tidy` inside peer directories, not at root.

**Code standards:**
- No package-level `var` for mutable state — use `RuntimeContext` (see `internal/runtime/conversation/runtime.go`)
- No `log.Printf` or `fmt.Println` — use structured logger from `RuntimeContext`
- Stage files by name (never `git add -A` or `git add .`)
- **Always run `make build` before committing**

**Layered build system:**
1. **Makefile** — dependency graph only. Targets declare deps and delegate. No multi-line shell.
2. **scripts/** — bash orchestration only. Sequencing, env, process management. No assertions.
3. **Go** — all logic, including test assertions and integration checks.

## `yc <verb>` Quick Reference

Reach for these before `grep`/`find`/`git`. When you don't, the agent-mode hint engine surfaces the better tool on stderr.

| Verb | Use when | Why instead of |
|------|----------|----------------|
| `yc symbols <path>` | Listing declarations (func, type, class) | `ctags -R`, `grep -E '^(func\|type)'` — treesitter-AST, no stale index |
| `yc repomap [--budget=N]` | Orienting in unfamiliar repo | `find . -name '*.go' \| xargs head` — token-budgeted file→symbol ranking |
| `yc search-symbols <pattern> [path]` | Name-substring search across identifiers | `grep -rn` — AST-aware, skips comments/string literals |
| `yc refs <symbol>` | Finding callers / references | `grep -rn 'FuncName('` — scopes to actual references |
| `yc git <subcmd>` | Git ops | system `git` — native go-git, no fork+exec; 3-tier fallback for unsupported verbs |
| `yc test [--json] [--framework <fw>]` | Running tests | parsing per-framework text — auto-detects, returns typed `TestResult` |
| `yc lsp <hover\|definition\|references\|symbols\|diagnostics> <file>[:line[:col]]` | Querying LSP | reading source manually — structured `Response` |
| `yc run [--json] -- <cmd>` | Commands where exit-code + duration matter | shell text parsing — typed envelope |
| `yc graph "<DQL>"` | Code knowledge graph queries | nothing else gives DQL — falls back to ephemeral mirror of `.agents/ycode/graph.json` |
| `yc remember "<text>"` | Saving facts for future sessions | ad-hoc notes — RRF-fused memex; auto-writes to `~/.claude/projects/<id>/memory/` when `$CLAUDE_PROJECT_DIR` set |
| `yc recall <query>` | Retrieving prior facts | grepping notes — searches both ycode and Claude corpora |
| `yc sandbox -- <cmd>` | Delegated sandbox command | running an external wrapper directly |
| `yc help` / `yc manifest` | Discovery | `yc manifest` emits JSON capability catalog |

**Discovery:** `ycode shell --suggest "<cmd>"` previews hints without executing. `ycode shell --manifest` is the full JSON catalog.

## Architecture

Key components:
- **Provider layer** (`internal/api/`) — Anthropic native + OpenAI-compatible
- **Tool registry** (`internal/tools/registry.go`) — always-available vs deferred (discovered via `ToolSearch`)
- **Config** (`internal/runtime/config/config.go`) — 4-tier merge: user → project → workspace → local
- **Permission modes** — ReadOnly → WorkspaceWrite → DangerFullAccess (declared in `ToolSpec.RequiredMode`)
- **Memex** (`pkg/memex/`) — five-layer memory system (KV, SQL, vector, graph, memo)
- **Agent-mode hints** (`internal/shell/agentmode/`) — regex-driven nudges fired on stderr when bash commands would be better served by `yc <verb>`

## Foreman / Worker Model

**You are the Foreman.** Full privileges: source tree, backlog at `~/.agents/ycode/projects/<id>/backlog/`, all MCP tools.

Workers are sandboxed subprocesses spawned via `/foreman` — they receive one Gitea issue and one Loom workspace.

**Planning:**
```bash
ycode backlog new "title" --priority p1|p2|p3
```

**Working:** If no specific task, run `/foreman` (or: `ycode backlog list --priority p1`).

Boss control: `ycode foreman pause/resume/stop/skip/prio/tell/status`

## Documentation

- `docs/strategy.md` — wedge positioning, feature-tier policy, graduation criteria
- `docs/usage.md` — CLI modes, configuration, tools, and workflows
- `docs/architecture.md` — full architecture, design decisions
- `docs/backlog.md` — Boss → Foreman → Worker protocol
- `docs/instructions.md` — shared agent-agnostic conventions, skill system, build/test/commit rules
- `docs/pipeline.md` — six-step development pipeline (research → plan → build/test → evaluate → commit → codify)

## Sub-directory Instructions

- `external/gitea/AGENTS.md` — embedded git server guidance
- `external/podman/AGENTS.md` — read-only historical container reference
- `peers/` modules have their own `CLAUDE.md` files

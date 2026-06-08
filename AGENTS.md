# AGENTS.md

This file provides guidance to AI coding assistants working in this repository.

## Project Overview

ycode — pure Go CLI agent harness. Single static binary, Go 1.26+, permissive-license dependencies only.

Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot.  
Core loop: `internal/runtime/conversation/runtime.go` — assemble request → provider → dispatch tool calls → repeat.

## First-Time Setup

```bash
make init                              # REQUIRED: submodules, Perses plugins, gzip assets, Gitea bindata
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY
make install-hooks                     # pre-push hook runs `make ci`
```

## Build & Test

```bash
make build           # full gate: tidy → fmt → vet → compile → test → verify
make compile         # quick compile only
make test            # unit tests (-short -race)
```

**Build tags** (see `Makefile`):
- Default: `sqlite,sqlite_unlock_notify,bindata`
- Manual: `go build -tags "sqlite,sqlite_unlock_notify,bindata" -o bin/ycode ./cmd/ycode/`

**Test patterns**:
```bash
# Single package / test
go test -short -race -run TestName ./internal/path/to/package/

# Never use bare `./...` — always exclude priorart/:
PACKAGES=$(go list ./... | grep -v '/priorart/')
```

## Critical Conventions

**Directory boundaries:**
- `priorart/` — **NEVER modify.** Read-only reference implementations.
- `external/` — vendored submodules. Do not modify directly.
- `peers/` — peer Go modules with own `go.mod`. Run `go mod tidy` inside peer directories, not at root.

**Code standards:**
- No package-level `var` for mutable state — use `RuntimeContext` (see `internal/runtime/conversation/runtime.go`)
- No `log.Printf` or `fmt.Println` — use structured logger from `RuntimeContext`
- Stage files by name (never `git add -A` or `git add .`)
- **Always run `make build` before committing**

## `yc <verb>` quick reference (reach for these before grep/find/git)

ycode ships in-process built-ins reachable as `yc <verb>` whenever your bash backend routes through `ycode shell -c` (e.g., a PATH wrapper at `~/bin/ycode-wrappers/bash`, or direct `ycode shell -c "..."` invocation). They beat the generic-shell equivalents on AST awareness, structured output, and process-fork overhead — reach for them by default. When you don't, the agent-mode hint engine surfaces the better tool on stderr (envelope `hints` array when `--json` is set), each hint carrying a one-line `Why:` so the substitution is informed, not reflexive.

Ordered roughly by ROI for code-exploration tasks — the gap between `yc <verb>` and generic-shell is widest at the top.

| Verb | Use when | Why instead of |
|------|----------|----------------|
| `yc symbols <path>` | Listing declarations (func, type, class) in a file or tree | `ctags -R`, `grep -E '^(func\|type)'` — treesitter-AST: no regex to guess, no comments matched, no stale index file |
| `yc repomap [--budget=N]` | Orienting in an unfamiliar repo | `find . -name '*.go' \| xargs head` — token-budgeted file→symbol ranking; surfaces the most informative files first |
| `yc search-symbols <pattern> [path]` | Name-substring/regex search across declared identifiers | `grep -rn` — AST-aware: skips comments and string literals, resolves Go/TS aliases, no vendored-copy false positives |
| `yc refs <symbol>` | Finding callers / references across the workspace | `grep -rn 'FuncName('` — scopes to actual references, not lexical matches |
| `yc git <subcmd>` | Read-only git ops (log/status/diff/branch/show/blame) and basic writes | system `git` — native go-git, no fork+exec; ~50–200ms saved per call on large repos. 3-tier fallback (native → host → container) for verbs go-git doesn't cover |
| `yc test [--json] [--framework <fw>]` | Running tests with structured pass/fail per case | parsing per-framework text (`go test` / `pytest` / `jest` / `vitest` / `cargo`) — auto-detects the framework, returns a typed `TestResult` |
| `yc lsp <hover\|definition\|references\|symbols\|diagnostics> <file>[:line[:col]]` | Querying a language server programmatically | reading source manually — drives the LSP, returns structured `Response` (`--json` for the raw payload) |
| `yc run [--json] -- <cmd>` | Builds / commands where exit-code + duration matter | shell text parsing — emits `{stdout, stderr, exit, duration_ms, command}` envelope |
| `yc graph "<DQL>"` | Read-only DQL queries over the code knowledge graph | nothing else in the shell gives you DQL over the codebase. Falls back to an ephemeral mirror of `.agents/ycode/graph.json` when no persistent DB exists, so it works even without `ycode serve` |
| `yc browser <open\|fetch\|find>` | Open / fetch / find on a web page | `curl` / `wget` — resolves redirects, sniffs content-type, handles JS-rendered pages |
| `yc tab <status\|extract\|screenshot\|navigate\|click\|type\|scroll\|back\|tabs>` | Driving the connected Chrome tab in live mode | headless wrappers — live mode targets the user's actual browser session via the local hub |
| `yc remember "<text>"` | Saving a fact / preference for future sessions | ad-hoc notes — RRF-fused memex semantic memory; auto-writes through to `~/.claude/projects/<id>/memory/` when `$CLAUDE_PROJECT_DIR` is set, so Claude Code and ycode share one corpus |
| `yc recall <query>` | Retrieving prior facts | grepping notes — same memex fusion, returns top-K with sources |
| `yc sandbox -- <cmd>` | Running untrusted/destructive commands | running on the host — podman-isolated, alpine, network=none, cwd mounted at `/workspace`. `YCODE_AUTO_SANDBOX=1` opt-in rewrites danger patterns automatically |
| `yc qacache <stats\|list\|clear>` | Inspecting the project Q→A cache that short-circuits repeated LLM calls | ycode-specific; no system equivalent |
| `yc help` / `yc manifest` | Discovery | `yc help` lists verbs for humans; `yc manifest` emits JSON capability catalog (built-ins, skills, sentinels, hint patterns) for agents |

**Notes on day-to-day use:**

- The system tool still works when its name overlaps (`yc git status` and `git status` both run); `yc git` saves the fork+exec. The hint engine on stderr surfaces these substitutions contextually.
- `ycode shell --suggest "<cmd>"` previews the hints a command would trigger without executing it.
- `ycode shell --manifest` is the full JSON capability catalog.
- Generic `grep`/`find`/`head` remain the right answer for config files, logs, and plain text. The above table targets the ~20% of cases where AST awareness or structured output would have saved a follow-up roundtrip — exactly where the muscle-memory-to-yc gap lives.

## Architecture

Key components:
- **Provider layer** (`internal/api/`) — Anthropic native + OpenAI-compatible
- **Tool registry** (`internal/tools/registry.go`) — always-available vs deferred (discovered via `ToolSearch`)
- **Config** (`internal/runtime/config/config.go`) — 4-tier merge: user → project → workspace → local
- **Permission modes** — ReadOnly → WorkspaceWrite → DangerFullAccess (declared in `ToolSpec.RequiredMode`)
- **Memex** (`pkg/memex/`) — five-layer memory system (KV, SQL, vector, graph, memo)
- **Agent-mode hints** (`internal/shell/agentmode/`) — regex-driven nudges fired on stderr (and the envelope's `hints[]` in `--json` mode) when a bash command would be better served by a `yc <verb>`. Each hint carries a `Why:` line

## Foreman / Worker Model

**You are the Foreman.** Full privileges: source tree, backlog at `~/.agents/ycode/projects/<id>/backlog/`, all MCP tools.

Workers are sandboxed subprocesses spawned via `/foreman` — they receive one Gitea issue and one Loom workspace.

**Planning:** Write backlog entries:
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

## Sub-directory Instructions

- `external/gitea/AGENTS.md` — embedded git server guidance
- `external/podman/AGENTS.md` — container engine integration
- `peers/` modules have their own `CLAUDE.md` files

# AGENTS.md

This file provides guidance to AI coding assistants working in this repository.

**Read [CLAUDE.md](./CLAUDE.md)** for Claude Code-specific conventions.

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

## Architecture

Key components:
- **Provider layer** (`internal/api/`) — Anthropic native + OpenAI-compatible
- **Tool registry** (`internal/tools/registry.go`) — always-available vs deferred (discovered via `ToolSearch`)
- **Config** (`internal/runtime/config/config.go`) — 4-tier merge: user → project → workspace → local
- **Permission modes** — ReadOnly → WorkspaceWrite → DangerFullAccess (declared in `ToolSpec.RequiredMode`)
- **Memex** (`pkg/memex/`) — five-layer memory system (KV, SQL, vector, graph, memo)

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

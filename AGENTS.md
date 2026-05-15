# AGENTS.md

Instructions for AI coding assistants working in this repository.

> **Scope:** These rules apply when an agent is operating *inside the ycode repo*. Sessions running under `ycode wrap -- <other-agent>` (e.g. `ycode wrap -- claude` against an unrelated project) are independent contexts — do not carry this repo's conventions into wrapped third-party sessions.

ycode — pure Go CLI agent harness. Single static binary, Go 1.26+, permissive-license dependencies only.

> **Start here:** [`docs/strategy.md`](./docs/strategy.md) — the wedge ("local-first, single-binary, runs offline"), feature-tier policy, graduation criteria.

## First-Time Setup

```bash
make init                              # REQUIRED: submodules, Perses plugins, gzip assets, Gitea bindata
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY
make install-hooks                     # recommended: pre-push hook runs `make ci`
```

## Build Quality Gate

```bash
make build           # full gate: tidy → fmt → vet → compile → test → verify
make compile         # quick compile (experimental features ON by default)
make compile-stable  # explicit opt-out: without experimental tag
make test            # unit tests only (-short -race)
make ci-fast         # quick CI check: verify-features + unit tests (skip Docker matrix)
```

**Build tags** (see `Makefile`):
- Default: `sqlite,sqlite_unlock_notify,bindata,experimental`
- Features are **ON by default** and opt-out — full opt-out policy and graduation criteria in [`docs/strategy.md`](./docs/strategy.md) (also linked at the top of this file)
- Manual: `go build -tags "sqlite,sqlite_unlock_notify,bindata,experimental" -o bin/ycode ./cmd/ycode/`

**Test patterns**:
```bash
# Single package / test (fast iteration)
go test -short -race -run TestName ./internal/path/to/package/

# Integration tests (needs container/network)
go test -tags integration -v -count=1 ./internal/integration/...

# Never use bare `./...` — always exclude priorart/:
PACKAGES=$(go list ./... | grep -v '/priorart/')
```

## Critical Conventions

**Directory boundaries:**
- `priorart/` — **read-only.** Never modify. Use `$(go list ./... | grep -v '/priorart/')` for Go commands.
- `external/` — vendored submodules. Do not modify directly.
- `peers/` — peer Go modules with own `go.mod`. Run `go mod tidy` inside peer directories, not at root.

**Code standards:**
- No package-level `var` for mutable state — use `RuntimeContext` (see `internal/runtime/conversation/runtime.go`)
- No `log.Printf` or `fmt.Println` — use structured logger from `RuntimeContext`
- Layered build system: logic in Go, orchestration in `scripts/`, dependency graph in `Makefile`
- No test logic in bash scripts — Go tests only
- If you edit `internal/features/registry.yaml`, run `make readme-features` — the README features block is generated from it

**Commits:**
- Stage files by name (never `git add -A` or `git add .`)
- Match repo prefix style: `fix:`, `feat:`, `docs:`, `refactor:`, `chore:`
- **Always run `make build` before committing** — must pass with no errors

## Architecture

Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot.

Core loop: `internal/runtime/conversation/runtime.go` — assemble request → send to provider → dispatch tool calls → repeat.

Key components:
- **Provider layer** (`internal/api/`) — Anthropic native + OpenAI-compatible
- **Tool registry** (`internal/tools/registry.go`) — always-available vs deferred (discovered via `ToolSearch`)
- **Config** (`internal/runtime/config/config.go`) — 4-tier merge: `~/.config/ycode/settings.json` → `~/.agents/ycode/projects/<id>/settings.json` → `<cwd>/.agents/ycode/settings.json` → `<cwd>/.agents/ycode/settings.local.json`
- **Permission modes** — ReadOnly → WorkspaceWrite → DangerFullAccess (declared in `ToolSpec.RequiredMode`)

## Foreman / Worker Model

**You are the Foreman.** Full privileges: source tree, backlog at `~/.agents/ycode/projects/<id>/backlog/`, all MCP tools.

**Workers** are sandboxed subprocesses spawned via `/foreman` — they receive one Gitea issue and one Loom workspace.

**Planning:** Write backlog entries:
```bash
ycode backlog new "title" --priority p1|p2|p3
```
Or directly: `~/.agents/ycode/projects/<id>/backlog/<slug>.md` with frontmatter `title`, `priority`, `state: open`. Markdown files are source of truth; Gitea is a cache.

**Working:** If no specific task, run `/foreman` (or: `ycode backlog list --priority p1`, then `ycode autopilot worker --issue N --loom-id ID`).

Boss control: `ycode foreman pause/resume/stop/skip/prio/tell/status`

## Running

```bash
bin/ycode                              # interactive REPL (auto-spawns server)
bin/ycode prompt "explain the runtime" # one-shot; add --print for plain text
bin/ycode serve                        # explicit server (Gitea, Ollama, SearXNG, NATS)
```

Server logs when auto-spawned: `~/.agents/ycode/observability/serve.log`

## CI Parity

```bash
make ci         # full GitHub Actions matrix in Docker (~5-10 min cold, ~2 min cached)
make ci-fast    # verify-features + unit tests only
```

Run `make ci` before push when touching CGO-adjacent code (podman/storage, sqlite, gpgme), workflow files, or `go.work`.

## References

- `docs/strategy.md` — wedge positioning, feature-tier policy, graduation criteria (read first for planning)
- `docs/roadmap.md` — tactical feature-gap inventory (P0/P1/P2)
- `docs/usage.md` — CLI modes, configuration, tools, and workflows
- `docs/instructions.md` — conventions, skill system, build/test rules
- `docs/architecture.md` — full architecture, design decisions
- `docs/backlog.md` — Boss → Foreman → Worker protocol
- `docs/memory.md` — five-layer memory system
- `docs/swarm.md` — agent orchestration
- `docs/lighthouse.md` — MCP federation for foreign agents

<!-- BEGIN YCODE -->
## ycode

This repo expects [ycode](https://github.com/qiangli/ycode) running locally.
See [`.agents/ycode/AGENTS.md`](.agents/ycode/AGENTS.md) for capability descriptions.
Run `ycode init --refresh` to update this section.

### Self-Bootstrap (Foreman role)

You are the **Foreman**. The Boss → Foreman → Worker protocol is universal.
Backlog state lives at `~/.agents/ycode/projects/<id>/backlog/` — shared across checkouts of the same repo.
When starting cold with no specific task, invoke `/foreman`.
Full protocol: [`docs/backlog.md`](docs/backlog.md).
<!-- END YCODE -->

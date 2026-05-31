# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`AGENTS.md` is the agent-agnostic counterpart — same project, slightly broader audience (Codex, OpenCode, Cursor). When the two diverge, treat this file as Claude-Code-specific overlay and `AGENTS.md` as the shared baseline.

## Project shape

ycode — pure Go CLI agent harness. Single static binary, Go 1.26+, permissive-license deps only (MIT/Apache-2.0/BSD).

- Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot.
- Core loop: `internal/runtime/conversation/runtime.go` — assemble request → provider → dispatch tool calls → repeat.
- This checkout usually lives inside the **`dhnt/` umbrella** as a git submodule. Sibling-path replaces in `go.mod` resolve `../sh` and `../nadir` to flat siblings — inside the umbrella those are real submodules; for standalone clones, `scripts/bootstrap-siblings.sh` reads `.sibling-pins` and clones them at the pinned SHAs.

## First-time setup

```bash
make init                              # REQUIRED: submodules, Perses plugins, Prometheus asset embeds, Gitea bindata
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY (+ optional OPENAI_BASE_URL)
make install-hooks                     # pre-push runs `make ci` in the ycode-builder Docker image
```

Skipping `make init` will leave Gitea bindata + Perses plugins missing; many tests and `ycode serve` will fail in subtle ways.

The embedded ollama runner (`internal/inference/runner_embed/ycode-runner.gz`) is produced by `make build` on first run via `runner-build-if-missing` — *not* by `make init`. On `darwin/arm64` no extra toolchain is needed (Metal is in-tree); other platforms need CMake + a C/C++ compiler. Without the toolchain the script warns and skip-cleans — ycode still builds but ollama features degrade to `ErrRunnerNotInstalled` at runtime. The embedded podman binary follows the same shape via `scripts/embed-podman.sh` (system binary first, fallback to a `-tags remote` build from `external/podman/cmd/podman/` on macOS/Windows or native on Linux).

## Escape hatch — `--use-system-binaries`

For devs who already have official upstream `ollama` and `podman` installed and want ycode to defer to them:

```bash
ycode --use-system-binaries serve              # globally use system binaries
ycode --use-system-binaries ollama list        # talk to user-run `ollama serve` instead of embedded
```

Or per-binary in `settings.json` (`~/.config/ycode/settings.json`):

```json
{
  "inference": { "useSystem": true },
  "container":  { "useSystem": true }
}
```

The CLI flag forces both to true at runtime (CLI flag > config > default). When set, ycode never extracts the embedded runner/podman, never auto-provisions a podman machine, and surfaces clean errors pointing at the opt-in choice when the user's system daemon isn't reachable — instead of silently spinning up the embed. ycode never *installs* upstream podman/ollama; users install them themselves.

## Build & test

```bash
make build           # full gate: tidy → fmt → vet → compile → test → verify
make compile         # quick compile only (no checks)
make test            # unit tests (-short -race) with default tags
make ci              # full GitHub Actions matrix in a Linux container (slow, definitive)
```

**Build tags** are non-trivial — the default is `sqlite,sqlite_unlock_notify,bindata` plus `embed_runner` auto-added when `internal/inference/runner_embed/ycode-runner.gz` exists and `embed_vfkit` auto-added when `internal/container/vfkit_embed/vfkit.gz` exists. The auto-add probes are in `Makefile:27`. Bare `go build` without tags will not produce a working binary. Use the Makefile or:

```bash
go build -tags "sqlite,sqlite_unlock_notify,bindata" -o bin/ycode ./cmd/ycode/
```

**Single test / package**:

```bash
go test -short -race -run TestName ./internal/path/to/package/
```

Never run bare `./...` — always exclude `priorart/`:

```bash
PACKAGES=$(go list ./... | grep -v '/priorart/')
```

Specialized test targets that require extra setup (each has prerequisites — read the Makefile comment before running):

- `make test-container` — podman required
- `make test-gitserver` — embedded Gitea, ~4 min
- `make test-tui` / `make test-tui-e2e` — TUI lifecycle; e2e needs a compiled binary and PTY
- `make test-ui` — Playwright (`cd e2e && npx playwright test`) against a running server
- `make eval-{contract,smoke,behavioral,e2e}` — eval tiers; `smoke`/`behavioral`/`e2e` need a live LLM provider

## Critical conventions

**Directory boundaries:**
- `priorart/` — **read-only.** Reference implementations of other agent harnesses (Aider, Cline, Codex, …). Never modify; never include in build/test globs.
- `external/` — vendored submodules (Gitea, podman, llama.cpp runner). Don't edit in place; bump the submodule SHA instead. Each has its own `AGENTS.md`.
- `peers/` — peer Go modules with their own `go.mod`. Run `go mod tidy` inside the peer directory, not at root.

**Code standards:**
- No package-level `var` for mutable state — thread `RuntimeContext` from `internal/runtime/conversation/runtime.go`.
- No `log.Printf` / `fmt.Println` — use the structured logger on `RuntimeContext`.
- Stage files by name (`git add path/to/file`). Never `git add -A` / `git add .` — the umbrella tree has loose artifacts and unrelated submodule pointers that must not get swept up.
- Run `make build` before committing anything non-trivial.

## Layered build system

The Makefile / scripts / Go split is enforced:

1. **Makefile** — dependency graph only. Targets declare deps and delegate. No multi-line shell.
2. **scripts/** — bash orchestration only. Sequencing, env, process management. No assertions or computation.
3. **Go** — all logic, including test assertions and integration checks.

Don't push test logic into bash, and don't grow shell blocks inside the Makefile.

## Architecture pillars

Three load-bearing systems — read these directories before making non-trivial changes:

- **Conversation runtime** (`internal/runtime/conversation/`) — the event loop; assembles prompt, dispatches tool calls, manages tool activation TTLs (`preactivate.go`).
- **Tool registry** (`internal/tools/registry.go`) — `ToolSpec` declares `RequiredMode` (ReadOnly / WorkspaceWrite / DangerFullAccess). Tools may be always-available or **deferred** — discovered at runtime via `ToolSearch` and loaded only when needed.
- **Memory** (`pkg/memex/`) — five-layer system (KV / SQL / vector / graph / memo) accessed through a single `Memex` facade. Don't reach into a single backend directly.

Supporting layers:

- **Provider layer** (`internal/api/`) — Anthropic native + OpenAI-compatible. `fallback.go` handles failover; `key_rotation.go` pools keys; `cache_warmer.go` keeps prompt caches hot.
- **Config** (`internal/runtime/config/`) — 4-tier merge: user → project → workspace → local. Later layers override earlier.
- **Permission modes** (`internal/runtime/permission/`) — enforced from `ToolSpec.RequiredMode`. Never bypass; if a tool needs more privilege than its current mode, raise the mode explicitly.
- **VFS** (`internal/runtime/vfs/`) — boundary-enforced filesystem. File-tool implementations go through this, not `os` directly.

Full deep dive: `docs/architecture.md`. Strategy and feature-tier policy (stable / experimental / wip): `docs/strategy.md`.

## Foreman / Worker

ycode runs a Foreman/Worker model when invoked through `/foreman` skills. The active session is the **Foreman** — full privileges, full source tree, backlog at `~/.agents/ycode/projects/<id>/backlog/`. Workers are sandboxed subprocesses, each pinned to one Gitea issue and one Loom workspace.

Useful commands:

```bash
ycode backlog new "title" --priority p1|p2|p3   # plan
ycode backlog list --priority p1                # see what's next
ycode foreman pause|resume|stop|skip|prio|tell|status
```

Protocol: `docs/backlog.md`. CLI/UX walk-through: `docs/usage.md`.

## Umbrella interaction

When this checkout is inside `dhnt/`, the parent `dhnt/CLAUDE.md` governs cross-cutting concerns (wire protocols, `MATRIX_*` envs, bearer-scope vocabulary, fleet upgrade). The submodule footgun: editing files inside `ycode/` and committing from the umbrella root commits the (unchanged) submodule pointer, not your edits. Always commit + push inside `ycode/` first, then bump the pin from the umbrella.

## Docs map

- `docs/strategy.md` — wedge positioning, feature-tier policy, graduation criteria (read for any planning or feature discussion)
- `docs/usage.md` — CLI modes, configuration, tools, workflows
- `docs/architecture.md` — full architecture deep dive
- `docs/instructions.md` — shared agent-agnostic conventions, skill system, build/test/commit rules
- `docs/backlog.md` — Boss → Foreman → Worker protocol
- `docs/pipeline.md` — six-step pipeline for non-trivial work (research → plan → build/test → evaluate → commit → codify)
- `external/gitea/AGENTS.md`, `external/podman/AGENTS.md` — embedded subsystem guidance

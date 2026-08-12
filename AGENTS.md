# AGENTS.md

Guidance for AI coding assistants working in this repository.

## Project Overview

ycode — pure Go CLI agent harness. Single static binary, Go 1.26+, permissive-license dependencies only.

Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot.  
Core loop: `internal/runtime/conversation/runtime.go` — assemble request → provider → dispatch tool calls → repeat.

## First-Time Setup

```bash
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY (+ optional OPENAI_BASE_URL)
make install-hooks                     # pre-push hook runs `make ci`
make compile                           # ~35s warm; binary at bin/ycode
```

**There is no required setup step.** `make compile` works on a fresh checkout inside the umbrella — the only embed this repo builds is the `ycode-spawn` micro-shim, produced automatically by `ensure-embeds`. Standalone clones need `scripts/bootstrap-siblings.sh` first, which materialises `../sh`, `../nadir`, and `../coreutils` at the SHAs in `.sibling-pins`.

**`make init` is currently broken** — it calls `scripts/build-gitea-frontend.sh`, which hard-exits because `external/gitea` no longer exists (see *Removed subsystems*). Do not run it, and do not document it as a prerequisite.

## Build & Test

```bash
make build           # full gate: tidy → fmt → vet → compile → test → verify
make compile         # quick compile only
make test            # unit tests (-short -race)
make install         # build + copy into $DHNT_BIN_DIR (shims deliberately NOT installed)
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
- `make test-integration` — `-tags integration`, requires a running server
- `make test-tui` / `make test-tui-e2e` — TUI lifecycle; e2e needs compiled binary + PTY
- `make test-ui` — Playwright (`cd e2e && npx playwright test`) against running server
- `make eval-{contract,smoke,behavioral,e2e,init}` — eval tiers; smoke/behavioral/e2e need a live LLM provider (`eval-contract` and `eval-init` are offline)
- `make bench-memory[-quality|-competitive|-latency|-all]` — memory-retrieval benchmarks (no LLM)
- `make verify-features` — runs `./internal/features/...`, asserting every path in `internal/features/registry.yaml` still exists. Part of `make build`, and the usual failure after moving or deleting a package.

**Stale targets:** `make test-gitserver` points at `./internal/gitserver/...`, which no longer exists — it fails, and so does `make test-all`, which depends on it.

## Critical Conventions

**Directory boundaries:**
- `priorart/` — **NEVER modify.** Read-only reference implementations.
- `external/` — vendored submodules (`jaeger`, `perses`, `victorialogs`). Not imported by the main module today; bump the submodule SHA rather than editing in place.
- `peers/` — peer Go modules with own `go.mod`. Run `go mod tidy` inside peer directories, not at root.

**Code standards:**
- No package-level `var` for mutable state — use `RuntimeContext` (see `internal/runtime/conversation/runtime.go`)
- No `log.Printf` or `fmt.Println` — use structured logger from `RuntimeContext`
- Stage files by name (never `git add -A` or `git add .`) — the umbrella tree carries loose artifacts and unrelated submodule pointers; `make compile` alone dirties `go.work.sum`
- **Always run `make build` before committing**

**Layered build system:**
1. **Makefile** — dependency graph only. Targets declare deps and delegate. No multi-line shell.
2. **scripts/** — bash orchestration only. Sequencing, env, process management. No assertions.
3. **Go** — all logic, including test assertions and integration checks.

## `yc <verb>` Quick Reference

Reach for these before `grep`/`find`/`git`. When you don't, the agent-mode hint engine surfaces the better tool on stderr.

**They are reachable ONLY through the shell** — there is no `ycode yc` subcommand (the binary answers `unknown command "yc"`). From a script it is `ycode shell -c "yc symbols …"`.

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
- **Conversation runtime** (`internal/runtime/conversation/`) — the event loop; assembles the prompt, dispatches tool calls, manages tool activation TTLs (`preactivate.go`)
- **Provider layer** (`internal/api/`) — Anthropic native + OpenAI-compatible; `fallback.go` (failover), `key_rotation.go` (key pool), `cache_warmer.go`
- **Tool registry** (`internal/tools/registry.go`, `specs.go`) — always-available vs deferred (discovered via `ToolSearch`)
- **Config** (`internal/runtime/config/config.go`) — 4-tier merge: user → project → workspace → local
- **Permission modes** (`internal/runtime/permission/`) — ReadOnly → WorkspaceWrite → DangerFullAccess (declared in `ToolSpec.RequiredMode`)
- **VFS** (`internal/runtime/vfs/`) — boundary-enforced filesystem; file tools go through this, not `os` directly
- **Memex** (`pkg/memex/`) — five-layer memory system (KV, SQL, vector, graph, memo) behind one facade; don't reach into a single backend
- **Feature registry** (`internal/features/registry.yaml`) — source of truth for feature tiers *and* their file paths; surfaced by `ycode features list|readme|verify`
- **Telemetry** (`internal/telemetry/`, `cmd/ycode/otel.go`, `internal/observe/`) — client-side OTEL export only, with **no loopback default** in collector resolution (`OTEL_EXPORTER_OTLP_ENDPOINT` > `serve` override > config > discovery file). `ycode serve` is deliberately lean (HTTP/WS API, optional embedded NATS, manifest, pprof); dashboards and storage come from an external collector such as `bashy otel`
- **Agent-mode hints** (`internal/shell/agentmode/`) — regex-driven nudges fired on stderr when bash commands would be better served by `yc <verb>`

Beyond the REPL and `prompt`, the command surface includes `serve` / `pulse` (background server), `shell`, `wrap` (launch a third-party agentic tool under ycode's shim PATH), `pair` (bearer token + config snippet for a foreign tool), `acp` (serve ycode as an ACP agent over stdio), `init`, `docs`, `heal`, `doctor`, `eval`. `ycode --help` is authoritative.

## Removed subsystems — do not resurrect from stale docs

Several large subsystems left this tree. Historical docs describing them survive; treat them as history, not instructions. If a doc tells you to run one of these, the doc is wrong.

| Gone from this tree | Where the job lives now |
|---|---|
| `ycode weave`, `pkg/loom`, `internal/gitserver`, `external/gitea` | `bashy weave` (`coreutils/pkg/weave`); playbooks are `bashy weave guide` + `coreutils/pkg/weave/{CONDUCTOR-PLAYBOOK,WEAVE-RUNBOOK}.md` |
| `ycode foreman`, `ycode backlog`, `internal/foreman`, `internal/backlog`, `skills/ycode-foreman/` | `bashy weave` below, the conductor playbook in `bashy/skills/conductor` above |
| MCP server/client (`docs/plan-remove-mcp.md`) | the `yc` shell verbs and the deferred tool registry |
| `internal/container`, `pkg/oci`, podman + ollama embeds | `coreutils/external/podman/engine`, `coreutils/pkg/{oci,ollm}`; ycode drives the shared isolated **`bashy`** podman machine |

Switching agents mid-session (`/agent`, `/tool`, `/detach`) is still here — see below.

Stale references still in the tree: `docs/backlog*`, `docs/loom-v2-*.md`, and the `test-gitserver` / `init` Makefile targets.

## Documentation

- `docs/strategy.md` — wedge positioning, feature-tier policy, graduation criteria
- `docs/usage.md` — CLI modes, configuration, tools, and workflows
- `docs/architecture.md` — full architecture, design decisions
- `docs/instructions.md` — shared agent-agnostic conventions, skill system, build/test/commit rules
- `docs/pipeline.md` — six-step development pipeline (research → plan → build/test → evaluate → commit → codify)
- `docs/shell-agent.md` — agent-mode shell integration recipes and the hint engine
- `docs/release.md` — release procedure and the per-platform asset matrix

## Sub-directory Instructions

- `peers/` modules have their own `CLAUDE.md` files

## Switching agents — /agent, /tool, /detach

ycode is the bridge BETWEEN agentic tools, not just one of them. From a
session you can hand the work to any agent in the fleet and the conversation
goes with it:

```
/agent                            list the fleet, grouped by capability band
/agent codex-gpt-5.5              attach — stay in ycode, its replies land here
/agent L4 --fresh                 strongest non-ycode agent, no context carried
/agent claude-opus4.8 --takeover  hand the terminal to its own full-screen UI
/tool codex                       switch by tool, using its configured model
/detach                           end the attached session, come back
```

The roster is the SAME embedded fleet catalog `bashy agents list` reads, and
`/agent` runs `bashy chat` underneath — bashy keeps ownership of agent
resolution, the credential firewall and sandboxing.

Attach is the default and carries the conversation; `--fresh` opts out. Every
switch asks the target to leave a handoff note before exiting; when one is
missing the transcript says so and labels the terminal scrape it does have as
a reconstruction rather than a verbatim record.

Full detail: `ycode docs agent-switching`.

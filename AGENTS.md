# AGENTS.md

Guidance for AI coding assistants working in this repository.

`CLAUDE.md` (Claude Code) and `GEMINI.md` (Gemini CLI) are the tool-specific counterparts. The three **duplicate** most of their content rather than layering, so a change to build commands, directory boundaries, code standards, the architecture list, the removed-subsystems table or the docs map must be made in all three — they have silently drifted before.

## Project Overview

ycode — pure Go CLI agent harness. Single static binary, Go 1.26+, permissive-license dependencies only.

Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot.  
Core loop: `internal/runtime/conversation/runtime.go` — assemble request → provider → dispatch tool calls → repeat.

## First-Time Setup

```bash
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY (+ optional OPENAI_BASE_URL)
bashy dag install-hooks                # pre-push hook runs `bashy dag ci`
bashy dag compile                      # ~4s warm; binary at bin/ycode
```

**There is no required setup step and no Makefile.** A fresh checkout inside the umbrella compiles as-is. Standalone clones need `scripts/bootstrap-siblings.sh` first, which materialises `../sh`, `../nadir` and `../coreutils` at the SHAs in `.sibling-pins`.

## Build & Test

```bash
bashy dag compile     # quick compile only (~4s warm); binary at bin/ycode
bashy dag build       # full gate: fmtcheck → vet → verify-features → compile → test
bashy dag test        # unit tests (-short -race), priorart/ excluded
bashy dag install     # build + copy into $DHNT_BIN_DIR (shims deliberately NOT installed)
bashy dag ci          # containerized matrix (slow, definitive)
bashy dag --list      # every target
bashy dag install-hooks  # one-time: pre-push hook runs `bashy dag ci`
```

`export ANTHROPIC_API_KEY=…` (or `OPENAI_API_KEY` + optional `OPENAI_BASE_URL`) before running the binary.

**There is no Makefile.** It was retired in favour of `DAG.md` + `bashy dag`; `scripts/gate.sh` is the same gate as one command, for contexts without bashy (the builder image, `dag ci`). Keep the three in step.

**The product binary has no build tags.** One binary, no variants. The old `TAG_LIST` carried `sqlite`, `sqlite_unlock_notify` and `bindata`, all three of which gate **zero files** in this tree — they were Gitea's, and Gitea left with `internal/gitserver`. `embed_spawn` selected a variant (embedded shim vs. the symlink fallback `spawn_embed.Available()` implements) and is not used by the release. So a bare `go build ./cmd/ycode/` now works and is what the pipeline runs. The tags you *will* meet are test-and-eval-only: `integration` (15 files across `internal/integration/` and `internal/cli/`), `e2e`, and `eval` / `eval_e2e` / `eval_behavioral` under `internal/eval/` — which is why `dag eval-init` passes `-tags eval` — plus the ordinary platform tags. And ignore the header comment in `internal/features/registry.yaml` promising `-tags experimental` / `-tags wip`: those gate zero files, so every tier compiles into the one binary and `tier:` is metadata for `ycode features`, not a compile switch.

**Releases are `bashy release`**, driven by `.goreleaser.yaml`: cross-compilation, `ycode-<os>-<arch>.tar.gz`, `SHA256SUMS` and a `bashy-release-v1` ledger. `bashy dag release-check` / `release-plan` / `release-snapshot`. **The asset names are load-bearing** — the umbrella's fleet-upgrade path resolves artifacts by name, so renaming one strands hosts.

**Single test / package** — never run bare `./...`; always exclude `priorart/`:

```bash
go test -short -race -run TestName ./internal/path/to/package/
PACKAGES=$(go list ./... | grep -v '/priorart/')
```

Targets that need setup (read the note in `DAG.md` first): `test-integration`, `test-tui`, `test-tui-e2e`, `test-ui`, `eval-contract`, `eval-init`, `bench-memory`. `verify-features` asserts every path in `internal/features/registry.yaml` still exists — it runs inside `build` and is the usual failure after moving or deleting a package.

## Critical Conventions

**Directory boundaries:**
- `priorart/` — **NEVER modify.** Read-only reference implementations.
- `external/` — vendored submodules (`jaeger`, `perses`, `victorialogs`). Not imported by the main module today; bump the submodule SHA rather than editing in place.
- `peers/` — peer Go modules with own `go.mod`. Run `go mod tidy` inside peer directories, not at root.

**Code standards:**
- No package-level `var` for mutable state — use `RuntimeContext` (see `internal/runtime/conversation/runtime.go`)
- No `log.Printf` or `fmt.Println` — use structured logger from `RuntimeContext`
- Stage files by name (never `git add -A` or `git add .`) — the umbrella tree carries loose artifacts and unrelated submodule pointers; `bashy dag compile` alone dirties `go.work.sum`
- **Always run `bashy dag build` before committing**

**Layered build system:**
1. **`DAG.md`** — dependency graph only. Targets declare deps and delegate.
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
- **Session / context window** (`internal/runtime/session/`) — the biggest subsystem in the tree (~30 files): token budgeting (`budget.go`, `context_window.go`), the compaction ladder (`microcompact.go` → `compact.go` → `compaction_retry.go` → `llm_summary.go`), pruning, transcript repair, `stuck_detector.go`, and the memory extract/prefetch bridge to `pkg/memex`. "The model lost context" / "the transcript is malformed" lives here, not in `conversation/`
- **Tool execution** (`internal/runtime/toolexec/`) — the layer under the registry that actually runs a tool call, choosing between a native-Go implementation and a subprocess. Native git lives in `coreutils/git` and is reached through `nativeGitFunc(...)` adapters, so a git bug is usually a coreutils bug; `stall_watchdog.go` kills a hung call
- **In-process subagents** (`internal/runtime/{swarm,team,agentpool,lanes,taskqueue,worker,cascade}`) — parallel sub-agents with a capacity governor and liveness classification (`agentpool/`), lane/queue scheduling, and `cascade/`, which climbs a model ladder when the current model has demonstrably stopped making progress. Distinct from `bashy weave`: that is cross-repo out-of-process orchestration, this is one ycode process fanning out
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

Stale references still in the tree: `docs/backlog*`, `docs/loom-v2-*.md`, and the retired `test-gitserver` / `init` targets. Note the table covers the *out-of-process* orchestration that left for `bashy weave` — the **in-process subagent** orchestration stayed and is listed under Architecture.

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

# GEMINI.md - Project Instructions for ycode

This file provides context and instructions for AI agents working on the `ycode` project.

`AGENTS.md` (shared baseline) and `CLAUDE.md` (Claude Code) are the counterparts. The three **duplicate** most of their content rather than layering, so a change to build commands, directory boundaries, coding standards, the architecture list, the removed-subsystems table or the docs map must be made in all three — they have silently drifted before.

## Project Overview

**ycode** is a pure Go CLI agent harness designed for autonomous software development. It aims to be a single static binary with only permissive-license dependencies (MIT, Apache-2.0, BSD).

### Core Technologies
- **Language:** Go 1.26+
- **Storage:** `modernc.org/sqlite` (pure Go) + bbolt; sessions and memex live under the ycode data dir
- **Messaging:** NATS (optionally embedded in `ycode serve`)
- **Observability:** OpenTelemetry export only — client-side, no embedded backend. Dashboards and storage come from an external collector such as `bashy otel`.
- **Inference / containers:** provided by the sibling `../coreutils` module (`pkg/ollm`, `external/podman/engine`, `pkg/oci`), **not** embedded in this repo.

### Architecture
- **Entry Point:** `cmd/ycode/main.go` using Cobra CLI.
- **Main App Loop:** `internal/cli/app.go` (REPL) and `internal/runtime/conversation/runtime.go`.
- **Registry:** Features are defined in `internal/features/registry.yaml` — the source of truth for feature tiers *and* their file paths. `bashy dag build` fails if a listed path disappears.
- **Sibling modules:** `go.mod` replaces resolve `../sh`, `../nadir`, and `../coreutils` as flat siblings (real submodules inside the `dhnt/` umbrella). `../coreutils` is the shared AgentOS hub and owns the code-intel engines that `internal/runtime/{treesitter,repomap,codegraph}` re-export via thin alias shims.
- **Session / Context Window:** `internal/runtime/session/` — the biggest subsystem here (~30 files). Token budgeting (`budget.go`, `context_window.go`), the compaction ladder (`microcompact.go` → `compact.go` → `compaction_retry.go` → `llm_summary.go`), pruning, transcript repair, `stuck_detector.go`, and the memory extract/prefetch bridge to `pkg/memex`. Context-loss and malformed-transcript problems live here, not in `conversation/`.
- **Tool Execution:** `internal/runtime/toolexec/` — runs a tool call, choosing a native-Go implementation or a subprocess. Native git lives in `coreutils/git`, reached via `nativeGitFunc(...)` adapters, so a git bug is usually a coreutils bug. `stall_watchdog.go` kills hung calls.
- **In-process Subagents:** `internal/runtime/{swarm,team,agentpool,lanes,taskqueue,worker,cascade}` — parallel sub-agents with a capacity governor and liveness classification (`agentpool/`), lane/queue scheduling, and `cascade/`, which climbs a model ladder when the current model has stopped making progress. Distinct from `bashy weave` (cross-repo, out-of-process); this is one ycode process fanning out.
- **Vendorized Deps:** Submodules under `external/` (`jaeger`, `perses`, `victorialogs` — not imported by the main module today) and read-only reference code under `priorart/`.

## Building and Running

**There is no Makefile** — it was retired in favour of `DAG.md` + `bashy dag`.
There is also **no required setup step**, and **the product binary has no build tags**: one binary, no variants, so a bare `go build ./cmd/ycode/` works. The tags you *will* meet are test-and-eval-only — `integration` (`internal/integration/`, `internal/cli/`), `e2e`, and `eval` / `eval_e2e` / `eval_behavioral` under `internal/eval/`, which is why `dag eval-init` passes `-tags eval` — plus the ordinary platform tags. Ignore the header in `internal/features/registry.yaml` promising `-tags experimental` / `-tags wip`: those gate zero files, so every tier compiles into the one binary and `tier:` is metadata for `ycode features`, not a compile switch.

```bash
bashy dag compile          # quick compile; binary at bin/ycode
bashy dag build            # full gate: fmtcheck → vet → verify-features → compile → test
bashy dag test             # unit tests (-short -race)
bashy dag install          # copy into $DHNT_BIN_DIR (shims deliberately NOT installed)
bashy dag ci               # containerized matrix
bashy dag --list           # every target
bashy dag install-hooks    # one-time: pre-push hook runs `bashy dag ci`
```

`scripts/gate.sh` is the same gate as one command, for contexts without bashy
(the builder image, `dag ci`). Standalone clones run
`scripts/bootstrap-siblings.sh` first.

**Releases** are `bashy release` over `.goreleaser.yaml` — cross-compilation,
`ycode-<os>-<arch>.tar.gz`, `SHA256SUMS`, and a `bashy-release-v1` ledger.
The asset names are load-bearing for the umbrella's fleet-upgrade path.

## Development Conventions

### Layered Build System
1. **`DAG.md`:** Dependency graph only. Targets declare deps and delegate.
2. **scripts/:** Bash orchestration (sequencing, environment, processes). No assertions.
3. **Go:** All logic, including unit/integration tests and assertions.

### Project Structure Rules
- **`internal/`:** Implementation details.
- **`pkg/`:** Reusable packages (`memex`, `ycode`). Root `go.work` is minimal — just `use .`, no workspace-level replaces.
- **`external/`:** Submodules. Do not modify directly; update the SHA.
- **`priorart/`:** **READ-ONLY.** Never modify these files.
- **`peers/`:** Local clones of related repos for side-by-side development (gitignored, absent by default). To activate one, add `./peers/<name>` to `go.work`'s `use` and run `go mod tidy` inside the peer, not at root.

### Coding Standards
- **No package-level mutable state:** Use `RuntimeContext`.
- **Structured Logging:** Use the logger from `RuntimeContext`, avoid `fmt.Println` or `log.Printf`.
- **Testing:**
  - Unit tests next to source in `*_test.go`. Use `testing.Short()` to skip slow tests.
  - Integration tests in `internal/integration/` with `//go:build integration`.
  - No test logic in Bash scripts.

### Git & Commits
- **Prefixes:** Use prefixes like `fix:`, `feat:`, `docs:`, `test:`.
- **Staging:** Stage files by name. **NEVER** use `git add .` or `git add -A`.
- **Pre-commit:** Always run `bashy dag build` before committing.

## Removed Subsystems — do not resurrect from stale docs

Several large subsystems left this tree. Historical docs describing them survive; treat them as history, not instructions.

| Gone from this tree | Where the job lives now |
|---|---|
| `ycode weave`, `pkg/loom`, `internal/gitserver`, `external/gitea` | `bashy weave` (`coreutils/pkg/weave`); playbook via `bashy weave guide` |
| `ycode foreman`, `ycode backlog`, `internal/foreman`, `internal/backlog` | `bashy weave` below, the conductor playbook in `bashy/skills/conductor` above |
| MCP server/client (`docs/plan-remove-mcp.md`) | the `yc` shell verbs and the deferred tool registry |
| `internal/container`, `pkg/oci`, podman + ollama embeds | `coreutils/external/podman/engine`, `coreutils/pkg/{oci,ollm}` |

Stale references still in the tree: `docs/backlog*`, `docs/loom-v2-*.md`, `docs/embedding-{gitea,podman}.md`, and the retired `test-gitserver` / `init` targets. Note the table covers the *out-of-process* orchestration that left for `bashy weave` — the **in-process subagent** orchestration stayed and is listed under Architecture.

## Agent-Specific Tools (`yc <verb>`)
These are in-process shell built-ins, **reachable ONLY through `ycode shell`** — there is no `ycode yc` subcommand (the binary answers `unknown command "yc"`). From a script it is `ycode shell -c "yc symbols …"`. Use these over standard Unix tools when possible:

| Command | Use For |
|---------|---------|
| `yc symbols <path>` | AST-aware symbol listing |
| `yc repomap` | High-level project orientation |
| `yc search-symbols` | Finding identifier definitions |
| `yc refs <symbol>` | Finding callers/references |
| `yc test` | Framework-aware test execution |
| `yc lsp <cmd>` | Querying Language Server Protocol |
| `yc remember/recall` | Managing agent memory |

## Documentation References
- `docs/strategy.md`: Feature-tier policy and operating principles.
- `docs/architecture.md`: Design decisions and component details.
- `docs/instructions.md`: Detailed shared conventions and skill system.
- `docs/pipeline.md`: The six-step dev pipeline (research → plan → build/test → evaluate → commit → codify).

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


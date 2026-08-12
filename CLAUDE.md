# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`AGENTS.md` is the agent-agnostic counterpart — same project, slightly broader audience (Codex, OpenCode, Cursor); `GEMINI.md` is the Gemini CLI flavor. When they diverge, treat this file as the Claude-Code-specific overlay and `AGENTS.md` as the shared baseline.

## Project shape

ycode — pure Go CLI agent harness. Single static binary, Go 1.26+, permissive-license deps only (MIT/Apache-2.0/BSD).

- Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot (`ycode prompt`).
- Core loop: `internal/runtime/conversation/runtime.go` — assemble request → provider → dispatch tool calls → repeat.
- This checkout usually lives inside the **`dhnt/` umbrella** as a git submodule. `go.mod` sibling-path replaces resolve `../sh`, `../nadir`, and `../coreutils` to flat siblings — inside the umbrella those are real submodules; for standalone clones, `scripts/bootstrap-siblings.sh` reads `.sibling-pins` and clones them at the pinned SHAs.
- `../coreutils` is the shared **AgentOS hub**: it owns the code-intel engines (`pkg/{treesitter,repomap,codegraph}`, which `internal/runtime/{treesitter,repomap,codegraph}` re-export via thin alias shims), the pure-Go git, the podman/OCI engine, and `pkg/ollm` (ollama). When a code-intel or container behavior looks wrong, the implementation is usually in coreutils, not here.
- Root `go.work` is minimal — just `use .`, no workspace-level replaces. To iterate on a `qiangli/*` dep alongside ycode, clone it under `peers/<name>` (gitignored, absent by default) and add `./peers/<name>` to `go.work`'s `use`.

## Build & test

```bash
make compile         # quick compile only (~35s warm); binary at bin/ycode
make build           # full gate: tidy → fmt → vet → compile → test → verify
make test            # unit tests (-short -race) with default tags
make ci              # full GitHub Actions matrix in a Linux container (slow, definitive)
make install-hooks   # pre-push hook runs `make ci` in the ycode-builder image
```

`export ANTHROPIC_API_KEY=…` (or `OPENAI_API_KEY` + optional `OPENAI_BASE_URL`) before running the binary.

**There is no required setup step.** `make compile` works on a fresh checkout inside the umbrella — the only embed this repo builds is the `ycode-spawn` micro-shim, produced automatically by `ensure-embeds`. **`make init` is currently broken**: it calls `scripts/build-gitea-frontend.sh`, which hard-exits because `external/gitea` no longer exists (see *Removed subsystems* below). Do not run it, and do not tell anyone it is a prerequisite.

**Build tags** are non-trivial. Default is `sqlite,sqlite_unlock_notify,bindata`, plus `embed_spawn` auto-added when `internal/runtime/wrap/spawn_embed/ycode-spawn.gz` exists — that auto-add is the single `TAG_LIST` line near the top of the `Makefile`. Bare `go build` without tags does not produce a working binary:

```bash
go build -tags "sqlite,sqlite_unlock_notify,bindata" -o bin/ycode ./cmd/ycode/
```

(`compile` re-invokes Make as a sub-make on purpose: `TAG_LIST`'s `$(wildcard …)` probes expand once at parse time, so embeds produced during the same invocation would otherwise be missing from the tag list.)

**Single test / package** — never run bare `./...`; always exclude `priorart/`:

```bash
go test -short -race -run TestName ./internal/path/to/package/
PACKAGES=$(go list ./... | grep -v '/priorart/')
```

Specialized targets (read the Makefile comment before running — each has prerequisites):

- `make test-integration` — `-tags integration`, requires a running server
- `make test-tui` / `make test-tui-e2e` — TUI lifecycle; e2e needs a compiled binary and a PTY
- `make test-ui` — Playwright (`cd e2e && npx playwright test`) against a running server
- `make eval-{contract,smoke,behavioral,e2e,init}` — eval tiers; `smoke`/`behavioral`/`e2e` need a live LLM provider (`eval-contract` and `eval-init` are offline)
- `make bench-memory[-quality|-competitive|-latency|-all]` — memory-retrieval benchmarks (no LLM)
- `make verify-features` — runs `./internal/features/...`, which asserts every path in `internal/features/registry.yaml` still exists. This is part of `make build` and is the usual failure after moving or deleting a package.

**Stale targets:** `make test-gitserver` points at `./internal/gitserver/...`, which no longer exists — it fails, and so does `make test-all`, which depends on it.

## Critical conventions

**Directory boundaries:**
- `priorart/` — **read-only.** Reference implementations of other harnesses (Aider, Cline, Codex, …). Never modify; never include in build/test globs.
- `external/` — vendored submodules (`jaeger`, `perses`, `victorialogs`). Not imported by the main module today; bump the submodule SHA rather than editing in place.
- `peers/` — peer Go modules with their own `go.mod`. Run `go mod tidy` inside the peer directory, not at root.

**Code standards:**
- No package-level `var` for mutable state — thread `RuntimeContext` from `internal/runtime/conversation/runtime.go`.
- No `log.Printf` / `fmt.Println` — use the structured logger on `RuntimeContext`.
- Stage files by name (`git add path/to/file`). Never `git add -A` / `git add .` — the umbrella tree carries loose artifacts and unrelated submodule pointers that must not get swept up. (`make compile` alone dirties `go.work.sum`.)
- Run `make build` before committing anything non-trivial.

## Layered build system

The Makefile / scripts / Go split is enforced:

1. **Makefile** — dependency graph only. Targets declare deps and delegate. No multi-line shell.
2. **scripts/** — bash orchestration only. Sequencing, env, process management. No assertions or computation.
3. **Go** — all logic, including test assertions and integration checks.

Don't push test logic into bash, and don't grow shell blocks inside the Makefile.

## Architecture pillars

Read these before non-trivial changes:

- **Conversation runtime** (`internal/runtime/conversation/`) — the event loop; assembles the prompt, dispatches tool calls, manages tool activation TTLs (`preactivate.go`).
- **Tool registry** (`internal/tools/registry.go`, `specs.go`) — `ToolSpec` declares `RequiredMode` (ReadOnly / WorkspaceWrite / DangerFullAccess). Tools are either always-available or **deferred** — discovered at runtime via `ToolSearch` and loaded only when needed (`deferred.go`, `availability.go`).
- **Memory** (`pkg/memex/`) — five-layer system (KV / SQL / vector / graph / memo) behind a single `Memex` facade. Don't reach into a single backend directly.
- **Feature registry** (`internal/features/registry.yaml`) — the source of truth for feature tiers (stable / experimental / wip) *and* their file paths; surfaced by `ycode features list|readme|verify`. Adding or moving a feature means editing this file, or `make build` fails.

Supporting layers:

- **Provider layer** (`internal/api/`) — Anthropic native + OpenAI-compatible. `fallback.go` handles failover; `key_rotation.go` pools keys; `cache_warmer.go` keeps prompt caches hot.
- **Config** (`internal/runtime/config/`) — 4-tier merge: user → project → workspace → local. Later layers override earlier.
- **Permission modes** (`internal/runtime/permission/`) — enforced from `ToolSpec.RequiredMode`. Never bypass; if a tool needs more privilege, raise the mode explicitly.
- **VFS** (`internal/runtime/vfs/`) — boundary-enforced filesystem. File-tool implementations go through this, not `os` directly.
- **Telemetry** (`internal/telemetry/`, `cmd/ycode/otel.go`, `internal/observe/`) — client-side OTEL export only. Collector address resolution is `OTEL_EXPORTER_OTLP_ENDPOINT` env > `serve`-injected override > config > discovery file, with **no loopback default** on purpose. `ycode serve` is deliberately lean (HTTP/WS API, optional embedded NATS, manifest, pprof); dashboards and storage come from an external collector such as `bashy otel`. The `query_traces` / `query_logs` / `query_metrics` tools (`internal/tools/otelstore*.go`) read that plane back.

Full deep dive: `docs/architecture.md`. Strategy and feature-tier policy: `docs/strategy.md`.

## Command surface

`ycode --help` is authoritative. Beyond the REPL and `prompt`, the surfaces worth knowing: `serve` / `pulse` (background server), `shell` (agentic bash — see below), `wrap` (launch a third-party agentic tool under ycode's shim PATH), `pair` (bearer token + config snippet for a foreign tool), `acp` (serve ycode as an ACP agent over stdio), `init` (establish ycode in a git repo, including foreign-tool configs), `docs` (embedded agent-facing capability prompts), `heal`, `doctor`, `eval`.

## `yc <verb>` quick reference

When your bash backend routes through `ycode shell -c`, the `yc <verb>` built-ins are in-process and unshadowable. **They are reachable ONLY through the shell — there is no `ycode yc` subcommand** (the binary answers `unknown command "yc"`). An E2E suite assumed otherwise and failed for months; to drive a verb from a script it is `ycode shell -c "yc symbols …"`. The canonical, ROI-ordered table with per-verb "why not grep/find/git" rationale lives in `AGENTS.md` — read it before reaching for `grep -rn`, `find . -name`, or `git log` on a code question. Highlights:

- **Code exploration**: `yc symbols` (declarations) → `yc repomap` (orientation) → `yc search-symbols` (AST-aware substring) → `yc refs` (callers).
- **Structured output**: `yc test --json`, `yc lsp <action> --json`, `yc run --json -- <cmd>` emit typed envelopes instead of per-tool text.
- **Memory bridge**: `yc remember` writes through to `~/.claude/projects/<project-id>/memory/` when `$CLAUDE_PROJECT_DIR` is set, so a fact saved in either tool surfaces in both. `yc recall` searches both corpora.
- **Hints**: `internal/shell/agentmode/` fires on stderr (and into `hints[]` in `--json` mode) when a bash command would be better served by a `yc` verb. Each hint carries a `Why:` line. `ycode shell --suggest "<cmd>"` previews without executing; `ycode shell --manifest` is the full JSON catalog.

## Switching agents — /agent, /tool, /detach

ycode is the bridge BETWEEN agentic tools, not just one of them. From a session you can hand the work to any agent in the fleet and the conversation goes with it:

```
/agent                            list the fleet, grouped by capability band
/agent codex-gpt-5.5              attach — stay in ycode, its replies land here
/agent L4 --fresh                 strongest non-ycode agent, no context carried
/agent claude-opus4.8 --takeover  hand the terminal to its own full-screen UI
/tool codex                       switch by tool, using its configured model
/detach                           end the attached session, come back
```

The roster is the SAME embedded fleet catalog `bashy agents list` reads — ycode has no second list to drift from. `/agent` runs `bashy chat` underneath, so bashy keeps ownership of agent resolution, the credential firewall and sandboxing.

**Attach is the default and carries the conversation**; `--fresh` opts out. That carried context is the point — `bashy chat --agent codex` from a terminal is one line, and going through ycode is only worth it because the work travels. Every switch asks the target to leave a handoff note before exiting; when one is missing, the transcript says so and labels what it does have (a terminal scrape) as a reconstruction rather than a verbatim record.

Full detail: `ycode docs agent-switching` (implementation: `internal/agentswitch/`).

## Removed subsystems — do not resurrect from stale docs

Several large subsystems left this tree. Historical docs describing them survive; treat them as history, not instructions. If a doc tells you to run one of these, the doc is wrong.

| Gone from this tree | Where the job lives now |
|---|---|
| `ycode weave`, `pkg/loom`, `internal/gitserver`, `external/gitea` | `bashy weave` (`coreutils/pkg/weave`); playbooks are `bashy weave guide` + `coreutils/pkg/weave/{CONDUCTOR-PLAYBOOK,WEAVE-RUNBOOK}.md` |
| `ycode foreman`, `ycode backlog`, `internal/foreman`, `internal/backlog`, `skills/ycode-foreman/` | `bashy weave` below, the conductor playbook in `bashy/skills/conductor` above |
| MCP server/client (`docs/plan-remove-mcp.md`) | the `yc` shell verbs and the deferred tool registry |
| `internal/container`, `pkg/oci`, podman + ollama embeds | `coreutils/external/podman/engine`, `coreutils/pkg/{oci,ollm}`; ycode drives the shared isolated **`bashy`** podman machine |

Stale references still in the tree: `docs/backlog*`, `docs/loom-v2-*.md`, the `test-gitserver` / `init` Makefile targets, and `AGENTS.md`'s "Sub-directory Instructions" pointing at `external/{gitea,podman}/AGENTS.md`.

## Skills

Bundled skills live at top-level `skills/` (`ycode-autopilot`, `ycode-claude`, `ycode-learn`, `ycode-oci`), are embedded in the binary via `skills/embed.go`, and install user-globally. Edit them there — not in `.agents/ycode/skills/`, which is the installed copy.

## Umbrella interaction

When this checkout is inside `dhnt/`, the parent `dhnt/CLAUDE.md` governs cross-cutting concerns (wire protocols, `MATRIX_*` envs, bearer-scope vocabulary, fleet upgrade). The submodule footgun: editing files inside `ycode/` and committing from the umbrella root commits the (unchanged) submodule pointer, not your edits. Always commit + push inside `ycode/` first, then bump the pin from the umbrella.

When `../sh`, `../coreutils`, or `../nadir` move, bump `.sibling-pins` (`scripts/update-sibling-pins.sh`) — an in-umbrella build resolves the siblings by path and silently masks the drift that breaks standalone clones.

## Docs map

- `docs/strategy.md` — wedge positioning, feature-tier policy, graduation criteria (read for any planning or feature discussion)
- `docs/usage.md` — CLI modes, configuration, tools, workflows
- `docs/architecture.md` — full architecture deep dive
- `docs/instructions.md` — shared agent-agnostic conventions, skill system, build/test/commit rules
- `docs/pipeline.md` — six-step pipeline for non-trivial work (research → plan → build/test → evaluate → commit → codify)
- `docs/shell-agent.md` — agent-mode shell integration recipes and the hint engine
- `docs/release.md` — release procedure and the per-platform asset matrix

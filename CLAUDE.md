# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`AGENTS.md` is the agent-agnostic counterpart — same project, slightly broader audience (Codex, OpenCode, Cursor); `GEMINI.md` is the Gemini CLI flavor. When they diverge, treat this file as Claude-Code-specific overlay and `AGENTS.md` as the shared baseline.

## Project shape

ycode — pure Go CLI agent harness. Single static binary, Go 1.26+, permissive-license deps only (MIT/Apache-2.0/BSD).

- Entry: `cmd/ycode/main.go` → cobra CLI → REPL (`internal/cli/app.go`) or one-shot.
- Core loop: `internal/runtime/conversation/runtime.go` — assemble request → provider → dispatch tool calls → repeat.
- This checkout usually lives inside the **`dhnt/` umbrella** as a git submodule. Sibling-path replaces in `go.mod` resolve `../sh`, `../nadir`, and `../coreutils` to flat siblings — inside the umbrella those are real submodules; for standalone clones, `scripts/bootstrap-siblings.sh` reads `.sibling-pins` and clones them at the pinned SHAs. `../coreutils` is the shared AgentOS hub: it now owns the code-intel engines (`pkg/{treesitter,repomap,codegraph}`, which ycode's `internal/runtime/{treesitter,repomap,codegraph}` re-export via thin alias shims) and the pure-Go git that loom/weave runs on.
- Root `go.work` is minimal — just `use .` (the main module). It has **no** workspace-level `replace` directives, and there are no `pkg/oci`/`pkg/otel` modules in this tree. Cross-repo resolution lives in `go.mod`'s sibling-path replaces (`mvdan.cc/sh/v3 => ../sh`, `github.com/qiangli/nadir => ../nadir`, `github.com/qiangli/coreutils => ../coreutils`). Ollama lives in the sibling `../coreutils` module (ycode imports `github.com/qiangli/coreutils/pkg/ollm`). To iterate on a `qiangli/*` dep alongside ycode, clone it under `peers/<name>` (gitignored, does not exist by default) and add `./peers/<name>` to `go.work`'s `use` directive.
- **Podman engine relocated to coreutils (2026-06-27, AgentOS Phase 4):** the in-process podman engine now lives in `../coreutils/external/podman/engine` (+ `../coreutils/pkg/oci`), consuming the `qiangli/podman` fork — coreutils is the **canonical home**, so `bashy podman` / outpost embed an isolated podman without ycode. **ycode consumes it:** `internal/container` is now a thin **alias shim** (`shim.go`) re-exporting `coreutils/external/podman/engine` (the `internal/runtime/{treesitter,repomap,codegraph}` precedent); the 35 duplicate impl files + the podman/vfkit/gvproxy embed dirs were deleted. (The one ycode-local piece that survived the move, `MCPHandler`, went with the MCP removal; `internal/container` no longer exists in this tree either.) The 17 consumers are unchanged (they still import `internal/container`); `go.work`/`go.mod` repoint `go.podman.io/podman/v6` + `coreutils/pkg/oci` at coreutils. **Behavior change:** ycode now drives the shared isolated **`bashy`** podman machine (was `ycode-default`). **ycode-revisit follow-ups:** ycode's Makefile embed-tag wiring (point at coreutils' blobs to re-embed podman in a fat `ycode`) + drop the now-orphaned `ycode/pkg/oci` (still used only by `cmd/ycode/podman_machine.go` + `examples/`). See dhnt/docs/agentos-substrate-extraction-plan.md + local-p2p-cicd.md.

## First-time setup

```bash
make init                              # REQUIRED: submodules, Perses plugins, Prometheus asset embeds, Gitea bindata
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY (+ optional OPENAI_BASE_URL)
make install-hooks                     # pre-push runs `make ci` in the ycode-builder Docker image
```

Skipping `make init` will leave Gitea bindata + Perses plugins missing; many tests and `ycode serve` will fail in subtle ways.

ycode's own Makefile builds exactly **one** embed: the `ycode-spawn` micro-shim (`make spawn-embed`, auto-run by `compile` via `ensure-embeds`). The ollama-runner and podman embed build machinery moved to **coreutils** (see the AgentOS Phase 4 note above) — there is no `make embed-fetch` or `runner-build-if-missing` target in this repo anymore. Runtime behavior is unchanged: ycode still extracts and drives the embedded runner/podman. On `darwin/arm64` no extra toolchain is needed (Metal is in-tree); other platforms need CMake + a C/C++ compiler to build the ollama runner, and without it ollama features degrade to `ErrRunnerNotInstalled` at runtime.

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

**Build tags** are non-trivial — the default is `sqlite,sqlite_unlock_notify,bindata`, plus `embed_spawn` auto-added when `internal/runtime/wrap/spawn_embed/ycode-spawn.gz` exists. That's the **only** auto-added tag now (the ollama/podman `embed_*` tags left with those engines when they relocated to coreutils). The auto-add probe is the single `TAG_LIST` line near the top of the `Makefile`. Bare `go build` without tags will not produce a working binary. Use the Makefile or:

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

- `make test-integration` — Go integration tests, requires a running server (`-tags integration`)
- `make test-gitserver` — embedded Gitea, ~4 min
- `make test-tui` / `make test-tui-e2e` — TUI lifecycle; e2e needs a compiled binary and PTY
- `make test-ui` — Playwright (`cd e2e && npx playwright test`) against a running server
- `make eval-{contract,smoke,behavioral,e2e,init}` — eval tiers; `smoke`/`behavioral`/`e2e` need a live LLM provider (`eval-contract` and `eval-init` are offline)
- `make bench-memory[-quality|-competitive|-latency|-all]` — memory-retrieval benchmarks (no LLM)

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

## `yc <verb>` quick reference

When your bash backend routes through `ycode shell -c`, the `yc <verb>` built-ins are in-process and unshadowable. **They are reachable ONLY through the shell — there is no `ycode yc` subcommand** (the binary answers `unknown command "yc"`). An E2E suite assumed otherwise and failed for months; if you want to drive a verb from a script, it is `ycode shell -c "yc symbols …"`. The canonical, ROI-ordered list with one-line "why use this instead of grep/find/git" rationale lives in `AGENTS.md` (§ `yc <verb>` quick reference) — see that table before reaching for `grep -rn`, `find . -name`, or `git log` on a code question. Highlights:

- **Code exploration**: `yc symbols` (declarations) → `yc repomap` (orientation) → `yc search-symbols` (AST-aware substring) → `yc refs` (callers).
- **Structured output**: `yc test --json`, `yc lsp <action> --json`, `yc run --json -- <cmd>` all emit typed envelopes instead of per-tool text formats.
- **Memory bridge**: `yc remember` writes through to `~/.claude/projects/<project-id>/memory/` when `$CLAUDE_PROJECT_DIR` is set, so a fact saved in either tool surfaces in both. `yc recall` searches both corpora.
- **Hints**: the agent-mode engine in `internal/shell/agentmode/` fires on stderr (and into the envelope's `hints[]` in `--json` mode) when a bash command would be better served by a `yc <verb>`. Each hint carries a `Why:` line — read it before re-running with the system tool.

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

## Switching agents — /agent, /tool, /detach

ycode is the bridge BETWEEN agentic tools, not just one of them. From a
session you can hand the work to any agent in the fleet and the conversation
goes with it:

```
/agent                            list the fleet, grouped by capability band
/agent codex-gpt-5.5              attach — stay in ycode, its replies land here
/agent Arlo                       the same agent, by nickname
/agent L4 --fresh                 strongest non-ycode agent, no context carried
/agent claude-opus4.8 --takeover  hand the terminal to its own full-screen UI
/tool codex                       switch by tool, using its configured model
/detach                           end the attached session, come back
```

The roster comes from the SAME embedded fleet catalog `bashy agents list`
reads — ycode has no second list to drift from. `/agent` runs `bashy chat`
underneath, so bashy keeps ownership of agent resolution, the credential
firewall and sandboxing.

**Attach is the default and carries the conversation**; `--fresh` opts out.
That carried context is the point — running `bashy chat --agent codex` from a
terminal is one line, and going through ycode is only worth it because the
work travels.

Every switch asks the target to leave a handoff note before exiting, and
ycode folds it into the transcript. When one is missing, the transcript says
so and labels what it does have (a terminal scrape) as a reconstruction
rather than a verbatim record.

Full detail, including provenance and limits: `ycode docs agent-switching`.

## Multi-agent orchestration — Weave and Foreman

**Weave** fans a queue of independent issues out to parallel subagent CLIs (claude, codex, opencode, gemini, …), each in an isolated git-clone sandbox, then converges with verification. **It has been re-homed out of ycode into the AgentOS hub** (`coreutils/pkg/weave`, pure-filesystem, no Gitea) and is now driven from the AgentOS shell as **`bashy weave …`**. Weave is **fully removed from ycode** — there is no `ycode weave` command. Its conductor playbook + runbook moved with the tool to `coreutils/pkg/weave/{CONDUCTOR-PLAYBOOK,WEAVE-RUNBOOK}.md`, and the terse discoverable version is `bashy weave guide`.

```bash
bashy weave add "title"                  # file an issue into the queue
bashy weave start [--issue N] [-- tool]  # claim, allocate sandbox, launch tool
bashy weave list                         # active weaves (TOOL / STARTED / DUR)
bashy weave log <N> [-f]                 # live PTY capture
bashy weave pull                         # merge submitted work back to main
```

The **loom** substrate (`pkg/loom` + `internal/gitserver/`) is **gone from this tree** — neither directory exists. Its only agent-facing surface was MCP (`loom_lease` / `loom_push` / `loom_status`), which went with the MCP removal (`docs/plan-remove-mcp.md`); the isolated-workspace job it did is now `bashy weave`. `docs/loom-v2-{plan,implementation}.md` describe deleted code and are kept as history only.

The operating playbook (blocked-agent protocol, verify-a-prompt-is-live-before-answering, full-suite regression gate after every round) lives **with the tool** now: `bashy weave guide` (terse, discoverable by any tool) + `coreutils/pkg/weave/{CONDUCTOR-PLAYBOOK,WEAVE-RUNBOOK}.md` (the rich playbook + worked example). Loom design: `docs/loom-v2-plan.md`.

**Foreman/Worker is GONE from this tree.** There is no `ycode foreman` and
no `ycode backlog` — the binary answers `unknown command`, and no
`internal/foreman` or `internal/backlog` package exists. It was removed
alongside loom/MCP; the isolated-workspace job it did is now `bashy weave`,
and the goal-driven layer above it is the conductor playbook in
`bashy/skills/conductor`.

Only historical documentation outlived the code (`docs/backlog*`) — treat it
as history, not instructions. The deprecated `skills/ycode-foreman/` bundle
was removed in 2026-08. This section previously listed `ycode backlog new` and
`ycode foreman pause|resume|…` as if they worked; four E2E tests asserted
against them and failed with `unknown command "backlog"` for however long
nobody ran `make ci`.

**Conductor** is the goal-oriented *director* that sits above all three:
it drives a team of agent CLIs through plan → research → fan-out → steer
→ converge → retro and loops until a goal's contract is verified done. It
does not replace weave/foreman/autopilot — it delegates to them. Unlike
those prose playbooks, conductor is authored as a runnable **dhnt skill**
(`github.com/dhnt/dhnt`, `skills/dev/conductor.go`, driven by
`dhnt conductor --goal "…" --verify "…"`): the goal is the contract, the
phases are steps, and a run emits a verifiable attestation. The general,
human-readable conductor playbook now lives in **bashy**
(`bashy/skills/conductor`), driving `bashy weave`/`bashy sprint` — it is no
longer bundled here.

## Skills

Bundled skills live at top-level `skills/` (`ycode-autopilot`, `ycode-learn`, …), are embedded in the binary via `skills/embed.go`, and install user-globally. Edit them there — not in `.agents/ycode/skills/`, which is the installed copy. (The general conductor playbook moved to `bashy/skills/conductor`; ycode can still drive the runnable dhnt `ConductorSkill` via `dhnt conductor`.)

## Umbrella interaction

When this checkout is inside `dhnt/`, the parent `dhnt/CLAUDE.md` governs cross-cutting concerns (wire protocols, `MATRIX_*` envs, bearer-scope vocabulary, fleet upgrade). The submodule footgun: editing files inside `ycode/` and committing from the umbrella root commits the (unchanged) submodule pointer, not your edits. Always commit + push inside `ycode/` first, then bump the pin from the umbrella.

## Docs map

- `docs/strategy.md` — wedge positioning, feature-tier policy, graduation criteria (read for any planning or feature discussion)
- `docs/usage.md` — CLI modes, configuration, tools, workflows
- `docs/architecture.md` — full architecture deep dive
- `docs/instructions.md` — shared agent-agnostic conventions, skill system, build/test/commit rules
- `docs/backlog.md` — Boss → Foreman → Worker protocol
- `docs/pipeline.md` — six-step pipeline for non-trivial work (research → plan → build/test → evaluate → commit → codify)
- `docs/loom-v2-plan.md` / `docs/loom-v2-implementation.md` — weave/loom v2 design and implementation status
- `docs/shell-agent.md` — agent-mode shell integration recipes and the hint engine
- `docs/release.md` — release procedure and the per-platform asset matrix
- `external/gitea/AGENTS.md`, `external/podman/AGENTS.md` — embedded subsystem guidance

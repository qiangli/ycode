# Loom v2 — Agent-Native Workspace Substrate

Status: design plan. Successor to the v1 contract in [`loom.md`](./loom.md).

This document defines the next iteration of Loom: a workspace substrate purpose-built for the case where a human user launches N agentic tools in parallel against one repo and walks away expecting safe convergence. v1 made parallel sub-agents possible; v2 makes them fool-proof and ergonomic.

## Why redesign

v1 ships a clean substrate but optimizes for *operators* (the parent agent that knows about leases). Three things consistently bite:

1. **Verb count and orchestration cost.** Five MCP verbs plus a hand-rolled polling loop is too much surface for sub-agents. Token cost is real and tool-selection accuracy degrades with registry size.
2. **No auto-attach.** The agent has to know it's inside Loom and call `loom_lease` to get started. Half the value of a substrate evaporates if the substrate is opt-in per-call.
3. **No fool-proof human path.** A human running three different agentic tools against the same repo has nothing in v1 to keep them from clobbering each other — at the working-tree level, the stash level, or the merge level.

v2 is the synthesis of three independent redesign takes plus the identity-model question, distilled to the smallest surface that achieves the goals.

## Design principles

These five principles drove every choice; everything below is derivation.

1. **Smallest verb surface that does the job.** Each tool in the registry is tokens in context and a decision point. Target: 3 verbs per *role*, not per system.
2. **Don't reinvent primitives the layer below already provides.** Git already has branches, diffs, cherry-picks, and rebases. The merger already runs CI. The orchestrator (Workflow, Foreman) already manages concurrency. Loom does one thing — isolated git workspaces with safe convergence.
3. **Auto-attach over self-orchestration.** The substrate puts the agent inside its workspace before its first token fires.
4. **Block over poll.** Token-billed loops never poll. Long verbs return terminal state; transitions stream as MCP resources.
5. **Smallest primitive that does the job.** For identity: user, not org. For backend: reference-clone, never worktree. For tier: opt-in, default to cheapest.

## Naming

The human-facing CLI lives under a new top-level command `ycode weave`.

`weave` pairs thematically with `loom` (the substrate): the loom holds the warp; weaving is the parallel-threads-converging-into-one-piece operation users actually do on it. The metaphor is consistent with ycode's existing naming style (`foreman`/`worker`, `pulse`, `mesh`, `outpost`) and captures both halves of the semantics — parallel work *and* convergence — that any synonym would only get half of.

| Surface | Command space | Audience |
|---|---|---|
| Human / orchestrator-agent front door | `ycode weave <subverb>` | end users *and* higher-level agents acting on their behalf |
| MCP agent surface | `loom_*` verbs + `loom://` resources | agents inside a workspace (sub-agent) or with MCP wired (orchestrator) |
| Substrate admin / debug | `ycode loom <subverb>` | developers debugging the substrate |
| Go package API | `pkg/loom` | internal callers (wrap, foreman, workflow) |

`ycode weave` and the MCP `loom_*` surface are **two equally-valid front doors to the same Go substrate.** An orchestrator that already speaks MCP prefers the MCP path (lower latency, structured streaming, no process spawn); an orchestrator stuck on shell-only (or any tool that can `execve`) uses `ycode weave`. Both are agent-friendly by design — see [Agent-friendly CLI](#agent-friendly-cli) for the conventions.

`weave` subverbs (full list):

```
ycode weave start --issue <N> -- <tool>     # allocate workspace, launch tool inside
ycode weave list                            # active weaves with loom-process delta
ycode weave pull                            # fast-forward user's main from merged work
ycode weave abandon <issue>                 # tear down one weave
ycode weave shell <issue>                   # drop into the sandbox for manual takeover
ycode weave open [--issue N | --pr | --board]  # open the right Gitea page in browser
ycode weave reset                           # nuke all weaves for this project
```

Common-case usage:

```
ycode weave start --issue 123 -- claude-code
ycode weave start --issue 124 -- opencode
ycode weave start --issue 125 -- codex
ycode weave list           # glance at progress
ycode weave pull           # absorb merged work into the user's checkout
```

The existing `ycode loom` top-level becomes the *substrate-admin* surface (developer/debug operations on leases — list raw lease state, force-reap, inspect store). It's not in the human's vocabulary; documentation points users at `weave`.

## Mental model

### Sub-agent's view

> "I'm in my workspace. I edit. I save points. I submit. If it conflicts, I fix the conflict and re-submit. If I give up, I abandon."

The sub-agent never sees a lease ID, sandbox path, slug, fork relationship, merger, or PR number. It sees a cwd it can write to and three verbs.

### Parent / orchestrator's view

> "I open workspaces, hand them to sub-agents, and watch transitions."

Parent never calls `loom_submit` or `loom_checkpoint` on its own behalf — those are sub-agent verbs. The parent may itself be a human (terminal) or an agentic tool (a higher-level orchestrator deciding to spawn N children); the same CLI and MCP surfaces serve both. See [Agent-friendly CLI](#agent-friendly-cli) for the conventions that make `ycode weave` consumable from a non-human caller.

### Human user's view

> "I run `ycode weave start --issue N -- <tool>` per terminal. The Gitea project board shows progress. `ycode weave pull` syncs merged work back. `ycode weave abandon` kills one."

The human's vocabulary is `weave`, `issue`, `tool`, `pull`, `abandon`. Loom, lease, sandbox, branch, PR, merger — none of these are in the human's mental model.

## Agent-facing surface

### Sub-agent role (default, auto-attached)

Active when `YCODE_LOOM_ID` is set in the environment.

| Verb | Purpose | Returns |
|---|---|---|
| `loom_checkpoint(summary?)` | Lightweight save point in the sandbox. Pure local git. | `{checkpoint_id, files_changed, lines_changed}` |
| `loom_submit(max_wait_seconds?)` | Push branch + open/refresh PR + block until terminal. Auto-rebases on conflict. | `{state, pr_url?, conflict_files?, ci_summary?}` |
| `loom_abandon(reason?)` | Tear down sandbox and (if no PR) branch. | `{abandoned: true}` |

Plus one MCP resource:

| Resource | Purpose |
|---|---|
| `loom://session` | SSE stream of state transitions for this session, with the current state delivered on connect. Subsumes status. |

No `loom_id` argument anywhere — it's bound to the session via env.

#### `loom_submit` semantics in detail

This is the load-bearing verb. Sequence:

1. Stage and commit everything in the sandbox using the lease's author identity (no-op commit if clean — existing HEAD is still pushed).
2. Push the branch upstream.
3. Open a PR against the target branch (or return the existing PR if one is open — idempotent).
4. Block until one of: `merged`, `ci_failed`, `conflict`, or the `max_wait_seconds` deadline (then return `pending`).

On `conflict`: substrate rebases the lease's branch onto current target-branch HEAD inside the sandbox automatically. Conflict markers are written into the conflicted files. The sandbox stays alive. The agent edits the files like any other file (same VFS, same edit tools) and calls `loom_submit` again. **No new verb. No "apply resolution" API. Conflict is recovered in place.**

On `ci_failed`: PR stays open; `ci_summary` includes the failing job and last 200 lines of output. Agent fixes and `loom_submit`s again.

### Parent / orchestrator role

Active when `YCODE_LOOM_ID` is *not* set.

| Verb | Purpose | Returns |
|---|---|---|
| `loom_open(label, mode?, readonly?, base?)` | Allocate a workspace. Returns id + path the parent can hand to a sub-agent. | `{loom_id, path, branch}` |
| `loom_terminate(loom_id, reason?)` | Forcibly kill a sub-agent's lease. | `{terminated: true}` |
| `loom_handoff(loom_id, spawn_command?)` | Convenience: spawn a sub-agent already attached to the workspace. | `{pid, agent_session_id}` |

Plus the resource scoped to all leases in the parent's project:

| Resource | Purpose |
|---|---|
| `loom://project` | SSE stream of every lease's transitions; the parent's monitoring channel. |

### Surface total

Six verbs total across the system. Each role sees three plus one resource. No agent ever sees both registries at once — selection is gated by environment at MCP-session start.

## Substrate (Go, internal — not MCP)

The existing v1 verbs (`Lease`, `Push`, `Merge`, `Status`, `Release`) survive *as the Go package API*. `ycode wrap`, `Foreman`, `Workflow`, and the orchestrator-role MCP layer call them. They are no longer agent-facing.

This fixes the v1 lease-store coupling: the worker no longer reads `~/.agents/ycode/observability/gitea/loom/leases.json` directly. It goes through the service. The dual path between `cmd/ycode/loom.go` and `cmd/ycode/autopilot.go` collapses to one.

```go
// Stable internal API; not on the MCP wire.
svc.Lease(ctx, LeaseRequest{...})           // existing
svc.Push(ctx, PushRequest{...})             // existing
svc.Release(ctx, ReleaseRequest{...})       // existing
svc.SubmitAndWait(ctx, SubmitRequest{...})  // new — block-with-deadline contract
svc.Rebase(ctx, RebaseRequest{...})         // new — internal to conflict recovery
svc.Watch(ctx, WatchFilter) <-chan LeaseEvent  // new — drives MCP resources
```

`PolicyLoom` in `internal/service/workspace.go` (currently erroring "not yet wired") becomes the wiring point for auto-attach.

## Sandbox isolation invariant

Two leases in the same project share **nothing** on disk except:

(a) the project's bare object database via git `alternates`, and
(b) the remote Gitea endpoint they push to.

They share no refs, no index, no stash, no reflog, no hooks, no config, no locks. An agent in one sandbox cannot observe — by any git operation — work-in-progress in another sandbox before it has been pushed to the remote.

This is **not** an implementation detail; it is the load-bearing property that makes the multi-agent multi-tool parallel case safe. Anything weaker (notably `git worktree`, which shares refs / stash / reflog / hooks / locks) reintroduces cross-agent interference that LLM agents are not equipped to handle.

### Implementation note: clones, not worktrees

Each sandbox is a `git clone --reference <local-bare-path> <gitea-clone-url>`. This gives:

- Per-clone refs, index, HEAD, stash, reflog, hooks, config.
- Object store shared with the local bare via `.git/objects/info/alternates`.

Result: sub-second lease setup, multi-GB-friendly disk footprint, full isolation. The substrate must not `git gc` the parent bare while children are alive.

`git worktree` is explicitly **not** used at the sandbox layer in either backend mode. It was designed for one human juggling branches, not N untrusting actors operating concurrently.

## Identity — three tiers, opt-in

Default to the cheapest; promote when the workload needs the next tier.

### Tier 1 — Ephemeral (default)

Single-admin Gitea user, single admin token, identity in branch name (`agent/agent-loom-<label>-<id8>/...`) and author trailer. v1's model, kept as-is. Zero Gitea object churn per lease. Fine for the 80% case: ephemeral parallel work on one repo with no external upstream and no cross-agent permission concern.

### Tier 2 — Scoped (opt-in per project or per lease)

Lazy per-`sub_agent_label` Gitea user. The first lease with label `extract-types` creates Gitea user `agent-extract-types`; every subsequent lease with that label reuses it. Each lease mints a fresh scoped PAT (push only to its branch, comment only on its PR). Object growth bounded by label diversity, not lease count.

Buys: persistent attribution across runs, real RBAC (sub-agent B literally cannot push to sub-agent A's branch), scoped credentials, clean external-upstream fork workflow.

### Tier 3 — Fleet (opt-in per task)

Per-task Gitea org with per-label users as members. Use when a coordinated multi-repo task needs every agent to see every other agent's forks. Rare; never the default.

### Tier choice

Per-project config (`loom.identity_tier`) overridable per-lease. The MCP surface is unchanged across tiers — agents do not see this knob.

### Why not org-per-agent

An org is a *container of users*, not an actor. Agents commit; commits have authors; authors are users. Per-agent org adds team plumbing for no actor-semantics gain and at fan-out load creates dozens of orgs an hour. User-per-label gives every property the org framing was reaching for (real identity, scoped tokens, multi-repo namespace, forks) with one Gitea row per label, not per lease.

## Backend modes — two, also opt-in

### `local` (default when no CI signals detected)

Reference-clone off a local bare repo (or the project's own `.git/`), no Gitea, no merger goroutine. Convergence by direct `git rebase main && git merge --ff-only main` against the local bare. Sub-second lease. Cleanup is `rm -rf` on the clone.

### `forge` (selected when CI signals detected)

Reference-clone from Gitea, full PR/CI/merger path. Required when the project has a real CI command, when external-upstream contribution is in scope, or when Tier 2/3 identity is requested.

Both modes use clones, never worktrees. The Gitea/no-Gitea axis is orthogonal to the clone/worktree axis; both modes pick clone.

### Auto-detection

- Has a `Makefile` with `test` target, or `.github/workflows/*`, or `loom.ci_command` configured → `backend: forge`.
- None of the above → `backend: local`.
- Single user, no team → `identity_tier: ephemeral`.
- Multi-user workspace or external upstream pairing → escalate to `scoped`.

Overridable in `.ycode/loom.yaml` per project.

## Lifecycle

- **Liveness, not last-verb-call, drives idle reaping.** The sub-agent's MCP session keepalive *is* the heartbeat. v1's 30-minute idle clock (which reaped sandboxes while agents were thinking hard) goes away. When the MCP session ends, idle starts; after the configured grace period (default 30 min), the lease is reaped if it has no open PR.
- **TTL stays as a hard ceiling** — same defaults (1h soft, 8h hard) — but only matters for orphaned leases. Live agents never hit it.
- **Pause/resume is not a verb.** A parent that wants to suspend a lease across MCP sessions passes `--keep-alive=<duration>` when terminating its session; Loom treats the lease as parked. If real workloads need explicit `loom_park` / `loom_unpark`, add later — don't bake it in now.
- **Reaper runs at the existing tick.** Open PRs cause sandbox-only reclaim; branch is left for the merger. Same as v1.

## Auto-attach (the load-bearing UX)

`ycode wrap --loom=auto` becomes the default for every sub-agent invocation path: foreman→worker, workflow→agent, parent-spawns-child, and `ycode weave start --issue N -- <tool>` for the human-facing case (see below).

By the time the sub-agent's first prompt fires:

- `cwd` is the sandbox.
- Environment carries `YCODE_LOOM_ID`, `YCODE_LOOM_BRANCH`, `YCODE_LOOM_BASE`, `YCODE_LOOM_LABEL`.
- The MCP registry is gated to the sub-agent role (3 verbs + 1 resource).
- The agent's edit tools route through the sandbox's VFS root automatically.
- A scoped PAT (Tier 2+) is in the environment.

The agent never knows it's inside Loom. It just notices it's in a git repo on a branch with a particular name. The whole substrate compresses into "the cwd you woke up in".

## Conflict recovery

Recovery is a path through the existing `loom_submit` verb, not new verbs.

1. Agent calls `loom_submit`.
2. PR creation/refresh succeeds; merger detects conflict at rebase time.
3. Substrate rebases the lease's branch onto current target-branch HEAD **inside the sandbox**.
4. Conflict markers are written into the conflicted files.
5. `loom_submit` returns `{state: conflict, files: [...], base_summary, head_summary, hint}`.
6. Agent edits the conflicted files using the same edit tools it always uses.
7. Agent calls `loom_submit` again. Loop until terminal.

No `loom_rebase`, `loom_conflict_context`, `loom_apply_resolution`, `loom_retry_merge`. Four verbs collapse to a structured return on the one verb that was going to be called anyway.

## Permissions

Three buckets, not one:

| Verb | Mode | Prompt content |
|---|---|---|
| `loom_checkpoint` | WorkspaceWrite | none (sandbox-local) |
| `loom_submit` | WorkspaceWrite + ProposeMerge | diff stats, target branch, commit list |
| `loom_abandon` | WorkspaceWrite | none |
| `loom_open` (parent) | WorkspaceWrite | label, target branch |
| `loom_terminate` (parent) | WorkspaceWrite | branch, has-open-PR flag |
| `loom_handoff` (parent) | WorkspaceWrite | label, command to spawn |
| `loom://session`, `loom://project` | ReadOnly | none |

`ProposeMerge` is a new permission. The gate's "ask once, allow forever this session" UX maps directly to one-prompt-per-session vs one-prompt-per-call control over the merge-proposing verb.

## Human-facing UX

### The front door

```
ycode weave start --issue 123 -- claude-code
ycode weave start --issue 124 -- opencode
ycode weave start --issue 125 -- codex
```

`ycode weave start`:

1. Looks up Gitea issue N in the embedded mirror; creates it with a minimal title if missing.
2. Allocates a Loom workspace bound to that issue.
3. Sets up auto-attach env.
4. Execs the named tool with that environment, in the sandbox.

Three terminals × one command = three isolated workspaces. By construction they cannot clobber each other's files — they don't share a filesystem path.

### Issues are Gitea issues

`--issue N` refers to **Gitea issue N** in the embedded mirror. Title, body, comments, labels all live in Gitea; the user can edit any of them through Gitea's UI and the changes flow back. ycode never duplicates the issue schema.

For projects whose upstream is GitHub (or another forge), a separate `ycode weave mirror-issues` step (out of scope for v2) syncs upstream issues into the local Gitea on demand. Pending that, the user creates issues in the local Gitea directly or lets `ycode weave start --issue` mint them.

### Three guarantees

These are the load-bearing promises to the human.

#### Guarantee 1 — The user's working tree is never an agent's cwd

No agentic tool launched through `ycode weave start` ever operates on the path the user cloned. The sandbox lives at `~/.agents/ycode/gitea/loom/sandboxes/<id>/`. The user's checkout is untouched until they explicitly `ycode weave pull`. Most of what could go wrong stops being possible.

Corollary: the user can keep editing their own checkout while three agents run. Their work and the agents' work don't share a filesystem; they only meet at merge time, in the local Gitea, where the merger can serialize them properly.

#### Guarantee 2 — Convergence is sequential, not racing

The local Gitea is the merge oracle. The merger processes PRs in arrival order. Per PR:

1. Rebase onto current `main` (no-op if main hasn't moved).
2. Run CI if configured.
3. Fast-forward merge. Main advances.
4. Next PR.

If rebase produces a conflict, the sub-agent's sandbox stays alive with markers; the auto-rebase-in-place contract on `loom_submit` lets the agent resolve and re-submit; merger re-tries.

The race "all three commit at once and clobber" cannot happen because they never share a working tree, and at merge time the merger is single-threaded per project.

#### Guarantee 3 — Lost work is impossible by default

Every workspace has continuous backing:

- Sandbox is a real git working tree with branches, reflog, stashes, commits — local-machine durable.
- `loom_checkpoint` (or any `git commit` the agent runs) goes to the sandbox immediately; nothing depends on the PR being open.
- The branch is pushed on every `loom_submit`; even if the PR never merges, the branch and its commits live in the local Gitea.
- The reaper only removes a sandbox after grace period **and** no open PR. The PR holds the branch alive even if the sandbox is reclaimed.
- A killed tool → orphaned lease → reaper handles it → branch and any pushed work are preserved for the user to recover via `ycode weave start --resume --issue <N>`.

### Dashboard

**The dashboard is Gitea.** We already embed Gitea for the forge; reuse its issue tracker, PR view, project board, labels, activity feed, webhooks, and notifications. No custom web UI.

First-run setup creates a `Loom` project board in the mirrored repo with columns mapped to loom states. Loom moves issues between columns by applying `loom:*` labels via the Gitea API. The user opens the board at `http://127.0.0.1:GITEA_PORT/admin/<repo>/projects/<n>` and sees their three tools' work as cards in the kanban, transitioning in real time. Click a card → standard Gitea issue page with comments, PR link, CI status, everything.

`ycode weave open --board` is the discoverability shortcut.

**The thin TUI overlay:**

```
$ ycode weave list
ISSUE  TOOL          STATE          SANDBOX                       HEARTBEAT  ACTION
#123   claude-code   working        ~/.agents/.../sandboxes/ab12  2s         —
#124   opencode      submitted      ~/.agents/.../sandboxes/cd34  8m         CI running
#125   codex         conflict       ~/.agents/.../sandboxes/ef56  1m         needs rebase resolution
```

The TUI exists only to show the **loom-process delta** — the columns Gitea doesn't know about: tool process name, sandbox path, MCP-session heartbeat. Everything else (issue title, PR state, CI status, comments) is one click into Gitea. State transitions pushed via the `loom://project` MCP resource — no polling.

Per-issue actions:

- `ycode weave shell <issue>` — drop into a shell already attached to that workspace.
- `ycode weave abandon <issue>` — tear down cleanly.
- `ycode weave open --issue <N>` / `--pr` — open Gitea page in browser.

**Loom posts to Gitea as it goes:**

- Each lease auto-updates a single sticky comment on its issue with current sandbox path, tool name, and heartbeat timestamp. So even in pure-Gitea-UI usage, the user sees the loom delta inline on the issue page without leaving Gitea.
- State transitions are label moves: `loom:working` → `loom:submitted` → `loom:ci-failed` | `loom:conflict` → `loom:merged` | `loom:abandoned`.
- Conflict context is a PR comment from the merger listing the conflicted files and the sandbox path.
- Merge commits carry `Fixes #N` so Gitea auto-closes the issue when the PR merges.

### Pulling converged work back

```
ycode weave pull             # fast-forward user's main from the local Gitea's main
ycode weave pull --watch     # daemon: fast-forward whenever a PR merges
```

The merger handles conflicts at PR-rebase time, so by the time work lands on Gitea's main, it's already linearized. `ycode weave pull` is always a fast-forward; it never produces conflicts in the user's working tree.

Uncommitted edits in the user's checkout are stashed before fast-forward. Committed divergence is reported, not silently merged.

### Defense in depth — five layers

The system has to assume the user will eventually do something wrong.

1. **Habit.** `ycode weave start` is the front door, in `--help`, in CLAUDE.md, and the documented selfinit entry.
2. **Integrated tools refuse to run unmanaged.** Through selfinit, integrated tools install a startup check: if cwd is a Loom-managed repo and `YCODE_LOOM_ID` is unset, the tool refuses with `Run via 'ycode weave start' to launch this tool against an isolated workspace.`
3. **Pre-commit hook in the user's working tree.** `ycode weave start` (first run, idempotent) installs a `pre-commit` hook that rejects any commit whose author email matches `*@ycode.local`. If a misconfigured tool somehow operates directly on the user's checkout and tries to commit as an agent, git refuses. The hook bumps a counter surfaced by `ycode weave list`.
4. **Merger sanity.** The merger refuses to fast-forward `main` past a commit whose committer is not in the expected per-project allowlist.
5. **Reaper grace + branch retention.** Even on cascading failure, branches are kept until the user explicitly purges. `ycode weave list --history` shows abandoned/expired leases for 7 days.

Five layers because the cost of each is low and any one alone could be subverted.

### First-run setup is invisible

The first `ycode weave start` in a fresh checkout:

1. Detects it's a git repo; if not, errors with `ycode weave requires a git repository`.
2. Mirrors the repo into the embedded Gitea as `admin/<slug>` (idempotent — checks if mirror exists first).
3. Installs the pre-commit hook (Layer 3).
4. Writes `.ycode/loom.yaml` (gitignored, machine-local) recording the project_id, default base branch, identity tier, and backend mode.
5. Creates the `loom:*` label set in the mirrored repo (one per state, colored).
6. Creates a `Loom` project board with columns: `working`, `submitted`, `ci_failed`, `conflict`, `merged`, `abandoned`.
7. Allocates the first lease and execs the tool.

Total wall-clock: 1–3 seconds for a small repo, 30s for a large one. The user sees a single "Setting up workspace…" line, then the tool launches. Every subsequent `ycode weave start` is sub-second.

No `ycode init`, no `ycode mirror`, no settings file the user has to author. Auto-detected config is overridable but not required.

### Failure modes the user can recover from

| What happens | What the user does | What the system does |
|---|---|---|
| Tool crashes mid-work | Nothing | Reaper cleans sandbox after grace; branch + commits preserved in Gitea; `ycode weave start --resume --issue <N>` reattaches |
| All three PRs conflict-cascade | Nothing — wait | Merger rebases each in order; sub-agents auto-resolve where possible; project board flags unresolvable ones |
| User edits their own checkout while agents work | Nothing | Pre-commit hook prevents agent-author commits on user tree; `ycode weave pull` stashes uncommitted edits before fast-forward |
| User wants to kill one | `ycode weave abandon 124` or move card to `abandoned` column in Gitea | Sandbox removed, branch removed if no open PR, lease closed |
| User wants to take over an agent's work | `ycode weave shell 123` | Drops into shell already in the sandbox with the lease's author identity active |
| User wants to discard everything and start over | `ycode weave reset` | All Loom leases for this project torn down; Gitea mirror preserved; user's working tree untouched |
| Local Gitea data lost | `ycode weave start` again | First-run setup re-mirrors from the user's checkout; lost work is exactly the PRs not yet pulled |

## Agent-friendly CLI

Every `ycode weave` subverb is designed for two consumers in equal measure:

- A human at a terminal.
- An agentic tool acting on behalf of a human — a higher-level orchestrator that decides to spawn N child tools, or a foreign agent harness that can only reach ycode via shell.

The CLI follows a small set of conventions so the agent path is as ergonomic as the MCP path. None of these are unique to `weave` — they're the standard ycode CLI conventions (see `internal/cli/`) — but they're load-bearing enough for `weave` that the contract is spelled out here.

### Output mode auto-detection

If stdout is a tty: pretty/colored output. If not (pipe, capture, agent context): plain text. Override:

- `--json` — machine-readable envelope (see schema below).
- `--plain` — no ANSI, no spinners, but still human-readable layout.
- `--quiet` — final result line only.

### Structured envelope

Every `--json` response is a single envelope with a versioned schema:

```json
{
  "schema_version": "loom-v2",
  "command": "weave start",
  "status": "ok",
  "result": {
    "loom_id": "loom-...",
    "issue": 123,
    "sandbox_path": "/.../sandboxes/ab12cd34",
    "branch": "agent/agent-loom-issue-123-.../free-...",
    "pr_url": null,
    "tool_pid": 12345
  },
  "hints": []
}
```

- `schema_version` is mandatory. Bump when output shape changes. Agents pin against the version they tested.
- `status` is one of `ok`, `error`, `partial`.
- `result` is verb-specific; the keys are stable across releases of the same `schema_version`.
- `hints` carries agent-mode suggestions (see [Hint stream](#hint-stream)).

For streaming verbs (`weave list --watch`), the envelope shape is per-event NDJSON with the same top-level keys plus `event_type` and `loom_id`.

### Stable exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic failure (IO, internal error) |
| 2 | Invalid invocation (bad flag, missing required arg) |
| 3 | Precondition not met (no tty for an interactive prompt, missing config) |
| 4 | State conflict (lease already exists with incompatible args, PR already merged) |
| 5 | External dependency unhealthy (Gitea down, network error to upstream) |

Agents branch on these; humans see the rendered error message.

### No interactive prompts in non-tty mode

If stdin is not a tty, no prompt blocks. Either:

- Use a sensible default (with the choice surfaced in `hints[]`), or
- Fail fast with exit code 3 and a structured hint explaining what was needed.

`--assume-yes` forces affirmative defaults across the board.

### Idempotency

`weave start --issue 123 --tool claude-code` is idempotent: if a weave for issue 123 already exists, **reattach** (return existing `loom_id`) rather than create a second. Same for `weave abandon` (no-op if already gone) and `weave pull` (no-op if already current). Agents can retry on transient failure without worrying about double-allocation.

### Streaming for watchable verbs

`ycode weave list --watch --json` emits one NDJSON event per state transition on stdout, terminating only on signal or `--max-events <N>`. Agents consume the stream directly without polling.

```
{"schema_version":"loom-v2","event_type":"transition","loom_id":"loom-...","from":"working","to":"submitted","ts":"..."}
{"schema_version":"loom-v2","event_type":"transition","loom_id":"loom-...","from":"submitted","to":"merged","ts":"..."}
```

### Hint stream

Per the shell agent-mode hint engine (`internal/shell/agentmode/`), when `weave` is called in a way the engine recognizes as suboptimal, it emits a structured hint on stderr and (in `--json` mode) also in `hints[]`. Examples:

- Calling `weave start` with a trailing `--` separator when `--tool` would compose better programmatically.
- Calling `weave list` in a polling loop when `--watch` would stream the same transitions.
- Calling `weave pull` without `--watch` repeatedly when a daemon would suffice.

Each hint carries a `why:` field so the agent can decide whether to adapt.

### Programmatic-friendly argument forms

Each verb accepts both human-natural form and programmatic form. They are equivalent:

```
ycode weave start --issue 123 -- claude-code      # human, mirrors `nice`/`xargs`/`time`
ycode weave start --issue 123 --tool claude-code  # programmatic, no `--` dance
```

Programmatic form avoids the `--` separator, which is awkward to construct in argv arrays from foreign code.

### Agent-context env var

`YCODE_AGENT=1` (or any truthy value) in the environment switches `ycode weave` to agent defaults: `--json`, `--plain`, no prompts, no spinners, no progress bars. The wrap layer sets it automatically when spawning a sub-agent so nested invocations behave consistently. Humans can also set it to force the agent-friendly output in scripts.

### CLI ↔ MCP relationship

The MCP `loom_*` verbs and the `ycode weave` CLI are not competitors — they're peer surfaces to the same Go substrate:

| Action | CLI | MCP |
|---|---|---|
| Allocate workspace + spawn tool | `weave start --issue N --tool T` | `loom_handoff(label, spawn_command)` |
| Allocate workspace only | `weave start --issue N --no-spawn` | `loom_open(label)` |
| List active leases | `weave list --json` | `loom://project` resource |
| Watch transitions | `weave list --watch --json` | `loom://project` resource (SSE) |
| Kill a lease | `weave abandon N` | `loom_terminate(loom_id)` |
| Pull merged work | `weave pull` | (no MCP equivalent — this is a human/orchestrator action, not a sub-agent's job) |

An orchestrator with MCP wired uses MCP. An orchestrator without (or one delegating to a shell tool) uses the CLI. Both end up calling the same `pkg/loom` Service; output schemas are aligned across surfaces where the operation is the same.

Note the one asymmetry: the CLI talks **issues** (Gitea issue number is the primary identifier), the MCP talks **loom_ids** (opaque substrate handle). The CLI does an extra issue→loom_id lookup; the MCP caller already has the handle. This is intentional — different abstraction levels for different audiences.

## What's deliberately excluded

Listing exclusions because the rationale matters more than the omission.

- **`loom_fork` / `loom_diff` / `loom_cherrypick`** — that's git. The sandbox is already a git repo. Don't reskin primitives.
- **`loom_pool_*`** — concurrency policy belongs to whoever spawns sub-agents (Workflow's concurrency cap, Foreman's worker pool). Coupling pool semantics to workspace allocation prevents Loom from composing with whatever scheduler the parent uses.
- **`loom_rewind`** — too dangerous on remote-pushed branches. The equivalent is achieved by checkpoint + `git reset` locally, which never touches remote.
- **`loom_pause` / `loom_resume`** — corner case, address via `--keep-alive` if it earns the verb.
- **Agent-reported progress / ETA fields** — fiction. Report only observable signals (files modified, last commit, last test result, last MCP-session heartbeat).
- **Test status in lease state** — the merger owns CI. Two paths running tests will diverge.
- **Multi-agent review verbs** — no underlying review primitive exists; ship when it does.
- **Task brief / `task_id` in `loom_open`** — task tracking is `ycode backlog`. `loom_open` accepts a `backlog_id` for cross-reference but does not duplicate the schema.
- **`cwd` as project key on the verb surface** — fragile (symlinks, worktrees, monorepo subdirs). Project identity is a stable `project_id`; `cwd` is the convenience input at registration time only.
- **Read-only verb option** — covered by `loom_open(..., readonly=true)` using a hardlink tree or reference-clone with checkout suppressed. No new verb; mode flag.
- **`git worktree` at the sandbox layer** — shares refs/stash/reflog/hooks/locks across leases. Reintroduces the cross-agent interference the substrate exists to prevent. See sandbox-isolation invariant above.
- **Custom web dashboard.** Gitea already provides issues, PRs, project boards, activity feeds, notifications, labels, webhooks, and a full REST API. Reuse it. `ycode weave list` exists only for the loom-process delta (tool name, sandbox path, heartbeat) that has no Gitea analogue.
- **Separate event bus / event-stream UI.** Gitea webhooks are the event bus. The `loom://session` / `loom://project` MCP resources are thin transformers over webhook payloads. No custom Kafka-shaped pipe.
- **Custom issue schema.** Issues are Gitea issues. `--issue N` is a Gitea issue number. ycode never invents a parallel schema with its own ID space.

## Migration from v1

Three releases, none breaking.

### N+0 (additive)

- Ship new MCP verbs (`loom_checkpoint`, `loom_submit`, `loom_abandon`, `loom_open`, `loom_terminate`, `loom_handoff`) and resources alongside v1 verbs.
- Old verbs gain a deprecation header.
- `ycode wrap --loom=auto` is opt-in via flag.
- Internal: route the worker through `Service` (fixing the `cmd/ycode/{loom,autopilot}.go` lease-store divergence) — no schema change, just removing direct-file reads.
- Wire `PolicyLoom` in `internal/service/workspace.go`.

### N+1

- `ycode wrap --loom=auto` becomes default-on.
- Tier 2 identity available behind config.
- `local` backend (reference-clone, no Gitea, no merger) ships behind config.
- `ycode weave` top-level ships with all subverbs (`start`, `list`, `pull`, `abandon`, `shell`, `open`, `reset`) as the human-facing front door.
- Gitea first-run bootstrapping (label set + `Loom` project board) lands as part of `ycode weave start` setup.
- Defense-in-depth Layers 2–4 wire in.

### N+2

- Old MCP verbs removed from the registry.
- Go API stays as the substrate.
- Tier 1 remains default; Tier 2/3 and `forge`/`local` selectable per project.

## The three load-bearing design moves

Strip away the table-stakes (dashboard, hooks, mirror-on-first-run, naming) and the load-bearing decisions are three:

1. **`ycode weave start --issue N -- <tool>` is the universal launcher, for humans *and* orchestrator agents.** Not `loom_lease`, not `ycode init`, not `claude-code` directly. One verb at the entry layer with the tool name as the trailing argument, mirroring `nice`, `time`, `xargs`. Every agentic tool plugs in by being execve-able; no cooperation required. The same surface serves a human at a terminal and a higher-level agent acting on a human's behalf — `--json`, idempotency, stable exit codes, and the `YCODE_AGENT=1` switch make the agent path as ergonomic as the human path. See [Agent-friendly CLI](#agent-friendly-cli).

2. **The user's working tree is downstream of the merge oracle, not upstream of it.** The user's `<repo>/main` is a read-only mirror of converged work; they pull. This inverts the usual git mental model (push from your tree) but it's the only shape that makes "N agents work in parallel without me thinking about it" actually safe. Convergence happens in the local Gitea where the merger serializes; the user's tree stays a pristine read-target.

3. **Gitea is the dashboard.** Issues, PRs, project boards, labels, comments, activity feeds, and webhooks all come from the forge we already embed. Loom's job is to keep Gitea state annotated correctly (label moves, sticky issue comments, `Fixes #N` trailers); the user's surface area then reduces to "open the Gitea board" plus a thin `ycode weave list` TUI for the loom-process delta.

Everything else — auto-attach, scoped credentials, pre-commit hook, three-tier identity, two-mode backend, reference-clone isolation — is implementation in service of those three design moves.

## Open questions

These are deliberately left for implementation to resolve:

- **Tier promotion trigger.** When a project hits a multi-actor or external-upstream signal, should promotion to Tier 2 be automatic or require explicit consent? Lean: explicit, with a one-line prompt the first time it would matter.
- **`local` backend dashboard.** Without Gitea, the "dashboard is Gitea" decision evaporates. Lean: `local` mode is for power users running single-repo light workflows; they get the TUI (`ycode weave list`) and no web UI. If they want the board, they're on `forge` mode.
- **`local` backend conflict path.** Without Gitea, where do conflict markers and "PR" state live? Lean: a `.loom/` dir at the bare-repo level holds per-branch state files; the merger goroutine still runs but reads/writes there instead of Gitea APIs.
- **`ycode weave pull` and submodules.** The dhnt umbrella case (every submodule pointer can move independently) makes "fast-forward main" ambiguous. Lean: `pull` operates at single-repo granularity; multi-repo orchestration is a higher-level concern.
- **Gitea project-board lifecycle.** When `ycode weave reset` runs, does it also delete the `Loom` project board and `loom:*` labels, or preserve them? Lean: preserve labels (cheap, useful history); delete the project board only if it's empty.
- **Upstream issue mirror.** `ycode weave mirror-issues` (one-shot sync from GitHub) is mentioned but not designed. Defer to a follow-up plan.

## References

- [`weave-runbook.md`](./weave-runbook.md) — end-to-end worked example (three agents on three issues, parallel work, pull, push to GitHub). The canonical how-to companion to this design doc.
- [`loom.md`](./loom.md) — v1 contract (current implementation).
- [`backlog.md`](./backlog.md) — task-tracking layer Loom integrates with via `backlog_id`.
- [`architecture.md`](./architecture.md) — overall ycode architecture.
- [`strategy.md`](./strategy.md) — feature-tier policy. Loom v2 ships as `experimental` in N+0/N+1 and graduates to `stable` in N+2.

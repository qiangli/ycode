# Multi-Agent Collaboration via Internal Gitea

Plan for running N autopilot agents against a single host codebase using ycode's
embedded Gitea (`internal/gitserver/`) as the coordination substrate. The host
codebase is whatever ycode is invoked against — could be ycode's own source, a
customer's React app, or anything else. The internal Gitea server is owned by
ycode end-to-end: smash-and-burn-safe state, fully reconstructible from the
user's working tree.

> **Status:** design only. No code written for this plan. See `docs/embedding-gitea.md`
> for the existing Gitea embedding it builds on.

## Goal

Let multiple autopilot agents work the same codebase in parallel without
stepping on each other's edits, without touching the user's working tree
unless explicitly told to, and with every agent's actions traceable through
the existing OTEL stack.

Concretely:

- Agents work in **forks** of an internal "upstream" mirror, not in cwd.
- Tasks come from a **prioritized queue** backed by Gitea issues.
- Auto-merge is gated on **local CI green**, not human review.
- Sync back to the user's cwd is **opt-in**, never automatic.
- Every agent run emits OTEL spans/logs/metrics tagged with `agent.id`.

## Org Layout (internal Gitea, per host project)

Each host project — keyed by absolute cwd path — gets one tracking repo
in the internal Gitea (`~/.agents/ycode/gitea/`):

```
admin/<slug>             # tracking repo:
                         #   refs/heads/main             — mirror of cwd HEAD
                         #   refs/heads/agent/<id>/...   — agent work branches
                         #   issues                      — the work queue
```

**Slug** = `<basename(cwd)>-<8-char-hash(abs-cwd)>` so two checkouts of the
same repo on disk don't collide.

**Why single-repo-with-branches** (revised from the original multi-org design).
Gitea's REST API as exposed by `internal/gitserver/api.go` doesn't support
org or per-agent user creation, and Gitea refuses fork-to-self in single-
user mode. The single-repo-with-branches scheme matches what we can implement
with the existing API surface and is equally effective in practice — agents
distinguish themselves by branch names (`agent/<id>/issue-<N>`) and the
git-author trailer; Gitea's fork-PR machinery would have added complexity
without changing semantics for the single-token deployment.

If/when we add per-agent Gitea users (deferred — see "Non-goals"), the org
layout can be revisited.

## Sync Model — cwd is the source of truth

The user's working tree is authoritative. `upstream/<slug>` is a mirror.

- **On `ycode autopilot collab` start** (or first time agents need a repo):
  create `upstream/<slug>` and `tasks/<slug>` if missing, then push cwd's
  current HEAD into upstream.
- **Periodically while agents run**: rebase `upstream/<slug>:main` onto the
  user's latest committed cwd state. Uncommitted changes are invisible to
  agents — same model as GitHub.
- **After PRs merge in Gitea**: append the merge SHA to a per-project
  pending-sync log. **Do not touch cwd automatically.** The user pulls when
  they want via `ycode tasks pull`. Default: no-op. (See "Sync-back policy"
  below for the conflict policy.)

This is the load-bearing rule: **ycode never modifies the user's working
tree without an explicit instruction in the task or a manual command from
the user.** Smash-and-burn of `~/.agents/ycode/gitea/` is always safe;
nothing in the user's cwd depends on it.

## Agent Identity & Observability

Single Gitea admin token for v1; agent identity rides in OTEL and git metadata,
not in Gitea's user table. Provisioning per-agent Gitea users is deferred until
we need their permission boundaries (we don't yet — fork ownership in the org
name is enough).

**Stable agent ID**: `agent-<8-char-uuid>` allocated at agent spawn,
persisted in the agent's task-registry entry (`internal/runtime/task/registry.go:36`).
Reused across iterations of the same agent's work.

**Git author trailer** on every commit:

```
Author: agent-<id> <agent-<id>@ycode.local>
```

So `git log` and Gitea's web UI attribute work correctly even though the
push token is shared.

**OTEL coverage** (the agent loop must be observable end-to-end; this is
how we'll debug stuck agents and reason about throughput):

- Every span emitted under an agent's loop carries baggage:
  `agent.id`, `agent.org`, `project.slug`, `task.issue_num`, `task.priority`.
- Structured logs (the `RuntimeContext` logger — never `log.Printf`, per
  AGENTS.md) include the same fields.
- Metrics:
  - `ycode_agent_iterations_total{agent_id, project_slug}` — counter
  - `ycode_agent_pr_total{agent_id, status="merged|conflict|abandoned"}` — counter
  - `ycode_agent_iteration_duration_seconds{agent_id, phase="research|build|test"}` — histogram
  - `ycode_agent_ci_runs_total{agent_id, result="pass|fail"}` — counter
  - `ycode_tasks_queue_depth{project_slug, priority}` — gauge

Hooks into the existing observability stack at `~/.agents/ycode/otel/` (the
collector, dashboards, and log retention policy already exist —
`cmd/ycode/serve.go:148`).

## Auto-merge on Green CI

"CI" here means **local** — the configured shell command(s) for the host
project, run in an isolated worktree. No GitHub Actions, no remote runners.

**Configuration** (in project settings — `<project>/.agents/ycode/settings.json`):

```json
{
  "tasks": {
    "ci_command": "make build",
    "ci_timeout_seconds": 1800,
    "auto_merge": true
  }
}
```

If `ci_command` is unset, fall back to detecting `make build`, then
`go test ./...`, then bail with a clear error.

**Merger loop** (separate from the agent loops, one per project):

1. Watch open PRs in `upstream/<slug>`.
2. For each PR: create a temp worktree at the **prospective merge commit**
   (Gitea's mergeable test SHA), run `ci_command`.
3. Green + no conflicts → call Gitea's merge API.
4. Red → comment the failure tail on the PR, label `ci-failed`, leave PR
   open; agent picks it up and iterates.

The merger reuses the existing worktree primitive (`internal/gitserver/workspace.go:38`)
and the in-process bash interpreter (`internal/runtime/bash/`), so CI runs
with the same security middleware that protects the rest of ycode.

**Race safety.** Two agents whose PRs touch the same lines: first to merge
wins; the second's CI run sees a conflict and the PR is rejected. The losing
agent reopens the issue (or picks a new one); no manual intervention.

## Sync-back Policy

The user's answer: **default to not touching cwd; let task instructions
opt in to push behavior**.

Three sync targets per merged PR, gated by what the task issue says:

| Mode                            | Where it goes                          | Trigger                                            |
| ------------------------------- | -------------------------------------- | -------------------------------------------------- |
| Internal-only (default)         | `upstream/<slug>:main` in Gitea        | Always                                             |
| Push to user-configured remote  | e.g. `origin` (GitHub) `:main`         | Issue label `push:origin` or task body `push: yes` |
| Pull into cwd                   | User's working tree                    | Manual `ycode tasks pull` only                     |

**Why opt-in.** Agents don't get to silently rewrite the user's working
tree, period. Touching cwd is the one action with real blast radius — the
rest (Gitea state, even external pushes from a flag the user set) is
recoverable or explicitly authorized.

**Conflict on `ycode tasks pull`**: stop, surface the conflict, exit
non-zero. Don't auto-stash, don't auto-rebase. The user resolves and re-runs.

**External push** (`push:origin` on the issue): the merger pushes the merged
SHA to the configured external remote *only* on green CI. The remote and
auth come from the host repo's existing `.git/config` — ycode does not
provision external credentials.

## Persistence

Reuses the existing footprint under `~/.agents/ycode/gitea/`:

- `gitea.db` — Gitea's SQLite (users, repos, issues, PRs, tokens). Already exists.
- `repositories/` — bare git repos, all orgs. Already exists.
- `projects.json` — **new**, one file: `{ "<abs-cwd>": { "slug": ..., "createdAt": ..., "lastSync": ... } }`.
- `pending-sync/<slug>.log` — **new**, append-only log of merge SHAs awaiting `tasks pull`.

That's the entire new persistence surface. Smash-and-burn `~/.agents/ycode/gitea/`
and the next `ycode autopilot collab` rebuilds from cwd in seconds.

## Components

### `internal/gitserver/projects/` (new)

Project registry and cwd↔Gitea mirror.

- `Resolve(cwd) → ProjectHandle` — looks up or creates the slug, ensures
  `upstream/<slug>` and `tasks/<slug>` exist.
- `MirrorUpstream(handle)` — push cwd HEAD into Gitea upstream.
- `RecordMerge(handle, sha)` — append to pending-sync log.
- `SyncBack(handle)` — fast-forward cwd from upstream; refuses on dirty cwd.

### `internal/gitserver/agents/` (new)

Per-agent fork lifecycle.

- `AssignFork(handle, agentID) → ForkHandle` — fork upstream into `agent-<id>/<slug>`
  via Gitea's fork API.
- `OpenPR(fork, branch, issueNum) → PRNum` — PR fork:branch → upstream:main, links issue.
- `Cleanup(fork)` — deletes the fork repo when agent retires.

### `internal/gitserver/queue/` (new)

Task queue over Gitea issues.

- `Pop(handle, agentID) → Issue` — atomic claim via Gitea label transitions
  (`p1` + `unassigned` → `p1` + `in-progress` + `assignee=agent-<id>`).
  Optimistic; retry on contention.
- `Submit(handle, title, body, labels) → Issue` — file work.
- `Complete(issue, prNum)` — close on PR merge.

### `internal/gitserver/merger/` (new)

Auto-merge daemon — one per project under `ycode autopilot collab`.

- Watches open PRs in upstream, runs CI, merges on green.
- Honors `tasks.ci_command` from project settings.
- Handles `push:origin` post-merge action.

### Wire-up (modify existing)

- `skills/autopilot/skill.md` — add `--collab` mode that scopes the agent's
  workspace to a fork rather than cwd.
- `cmd/ycode/autopilot_collab.go` (new) — CLI entry: `ycode autopilot collab --agents N`.
- `cmd/ycode/tasks.go` (new) — CLI: `ycode tasks add | list | pull`.
- `internal/gitserver/api.go` — add `ForkRepo` if absent (Gitea has the endpoint).

### What's already there (reused, not rebuilt)

- `internal/gitserver/workspace.go:38` — `PrepareWorkspace` becomes the
  "checkout fork into a sandbox dir" primitive for both agents and merger.
- `internal/runtime/toolexec/git_native_tier2.go` — native `Push`/`Pull`/`Fetch`.
- `internal/gitserver/api.go` — REST client (issues, PRs, branches, merges).
- `internal/runtime/task/registry.go:36` — background goroutine tracking.
- `internal/runtime/bash/` — in-process CI execution.
- `~/.agents/ycode/otel/` — observability collector + retention.

## Phases

Each phase ships independently and is verifiable end-to-end on its own.

1. **Project registry + mirror** — `projects/` package, slug + upstream creation,
   `mirror up` and `tasks pull`. CLI: `ycode tasks-internal mirror`. No agents.
   *Useful on its own as a backup/audit log of cwd into the internal Gitea.*

2. **Fork lifecycle + PR** — `agents/` package. CLI: `ycode tasks-internal fork --as alice`,
   then push/PR by hand. *Validates Gitea's fork+PR API works programmatically
   and that the OTEL agent.id baggage flows correctly.*

3. **Issue queue** — `queue/` package + `ycode tasks add/list`. Manual agent
   invocation still. *Validates queue claim atomicity under contention with a
   stress test (N goroutines hammering Pop).*

4. **Merger + auto-merge** — `merger/` package. Standalone daemon: file an
   issue, manually push a green branch, watch it merge automatically. *First
   end-to-end "green CI → merge" without an agent in the loop.*

5. **Autopilot --collab** — glue layer wiring the agent loop. `--agents N`
   spawns N agents; each runs the existing autopilot skill scoped to a fork.
   *First real multi-agent run, on ycode itself as the test bed.*

6. **Third-party validation** — run against an unrelated repo (something
   small and Go-free, e.g. a Node app) to confirm cwd-as-source-of-truth
   works without ycode-isms leaking in. *The most important phase — proves
   the design isn't accidentally coupled to ycode's own build.*

## Non-goals (v1)

- **Per-agent Gitea users.** Single admin token; identity in OTEL + author trailer.
- **Cross-project agents.** One autopilot collab process serves one cwd.
- **GitHub Actions / remote CI.** Local CI only.
- **Auto-stash / auto-rebase on `tasks pull`.** Conflict → stop and surface.
- **Smart conflict resolution between concurrent agent PRs.** First merge wins; second is rejected; agent picks new work.

## Open questions deferred to implementation

- **Pending-sync log format**: append-only text or SQLite table? Lean text for now (one SHA per line + timestamp), promote if we need queries.
- **Merger debounce**: how long to wait after a push before running CI, to coalesce rapid pushes from the same agent? Start with 5s; tune.
- **Agent retirement policy**: idle timeout? Max iterations? Lifetime budget? Open until we see real run data.

## References

- `docs/embedding-gitea.md` — how Gitea is embedded in-process (the foundation this builds on).
- `docs/swarm.md` — multi-agent orchestration primitives (handoff, flow types). This plan is the *workflow* layer; swarm.md is the *agent-definition* layer.
- `docs/autonomous-loop.md` — RESEARCH→PLAN→BUILD→EVALUATE→LEARN. Each agent's inner loop in `--collab` mode is a single iteration of this loop.
- `internal/gitserver/server.go:42` — Gitea data dir.
- `internal/gitserver/api.go` — REST client.
- `internal/gitserver/workspace.go:38` — existing worktree primitive.
- `cmd/ycode/serve.go:148` — observability data dir.

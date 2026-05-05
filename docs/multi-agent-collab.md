# Multi-agent collaboration on the same repo

> Exploration / one-pager. Not yet a build commitment. See [Open questions](#open-questions) and [Strategic placement](#strategic-placement) before scoping.

## TL;DR

Spawn N agents working on the same repo simultaneously, each isolated in its own **podman container** with its own branch + worktree, coordinating through ycode's embedded Gitea. PRs are the handoff primitive (the same one humans use); merge conflicts are real conflicts; "done" means the integrator agent (or a human) merges the PR. Almost all the mechanics already exist in ycode — embedded podman + embedded Gitea + native go-git is genuinely a unique combination — the net-new is the orchestrator, the task queue, and the integrator role. This is **Phase 4 differentiator territory** at minimum, and arguably wedge-strengthening: "your repo, your agents, your sandbox, your data — one binary, all local."

## Why ycode is uniquely positioned

| Capability | Ycode has | Competitors |
|---|---|---|
| Embedded git server | `internal/gitserver/` (Gitea) | None do |
| **Embedded podman** for sandboxed execution | `internal/container/` | None do |
| Native go-git ops (branch, worktree, push, PR) | `internal/runtime/toolexec/` (31 NativeFuncs) | Some (via shell) |
| Agent mesh / bus | `internal/mesh/` (currently `experimental`) | A few |
| Single binary, no external services | Yes | Almost none |

The combination of **embedded Gitea + embedded podman** is the load-bearing differentiator. Every other agent that wants multi-agent collaboration has to assume external infrastructure: Docker installed on the host, a remote GitHub/GitLab account, network access to cloud services. ycode does it with one binary on a laptop, even on a plane.

A team-of-agents that runs entirely on your laptop with no cloud dependency reinforces the local-first wedge — "your repo, your agents, your sandbox, your data — one binary, all local."

## Design space

| Pattern | What it does | When to use | Tradeoff |
|---|---|---|---|
| **Branch-per-agent + PR handoff** *(recommended)* | Each agent works on its own branch in its own worktree; merges via PR | General-purpose; embarrassingly parallel + decomposable | Conflicts surface at merge; needs an integrator |
| **File-locking via protected branches** | Lock files an agent is editing | Real-time concurrent edits to same files | Heavy; rarely needed if decomposition is good |
| **Plan-then-divide (orchestrator + workers)** | One LLM splits a goal, N workers execute pieces | Big tasks decomposable into independent subtasks | Decomposition quality bounds outcome |
| **Pipeline (researcher → coder → reviewer)** | Sequential stages, not concurrent | Tasks with natural phase order | Not parallel speedup; existing skill chains cover this |

**Recommendation: ship Branch-per-agent + PR handoff first; layer Plan-then-divide on top once that works. Skip file-locking entirely (good decomposition + PRs avoid the need).**

## Recommended shape (concrete walk-through)

```
ycode swarm "split README into per-provider sub-files (anthropic, openai, ollama, xai)"
  │
  ▼
Orchestrator (LLM): decomposes into 4 tasks, queues them
  │
  ▼
Workers 1-4 spawn in parallel, each as its own podman container:
  ─ each: container with --cpus 2 --memory 2g, network namespace
  ─ each: bind-mounts own worktree at /workspace (host: .agents/ycode/worktrees/<id>/)
  ─ each: gitea push of new branch agent/<id>/split-readme-<provider>
  ─ each: own session + episodic memory bound to its branch
  ─ each: ycode runtime inside the container; edits, runs tests, commits
  ─ each: cannot see other agents' worktrees or process tree (cgroup + ns isolation)
  │
  ▼
Each worker: pushes branch to embedded Gitea (via shared Gitea socket /
  port-forward), opens PR against main
  │
  ▼
Integrator agent: reviews PRs (test results + LLM judgement), sequences
  merges to avoid conflicts (e.g., README-anchor PR first), or kicks
  back PRs that conflict for the worker to rebase
  │
  ▼
Final state: 4 commits on main; 4 branches archived; 4 containers torn down;
  transcript per agent preserved in episodic memory for replay/audit.
```

## What gets reused (existing scaffolding)

| Need | Reuses | Path |
|---|---|---|
| **Sandboxed agent execution** | Embedded podman; existing `ContainerExecutor` for bash | `internal/container/`, `internal/runtime/bash/` |
| Branch + worktree + push + pull | Native go-git NativeFuncs | `internal/runtime/toolexec/` |
| Local git remote | Embedded Gitea | `internal/gitserver/` |
| PR / issue API | Gitea's GitHub-compatible API | `internal/gitserver/` + `internal/runtime/github/` (which speaks the same protocol) |
| Agent spawning | Subagent tool, parallel-agents, team | `internal/tools/` agent handlers |
| Inter-agent messaging | NATS mesh bus (`experimental` — graduate as part of this work) | `internal/mesh/` |
| Per-agent memory & transcript | Episodic JSONL session, branch-bound | `pkg/memex/memory/` |
| Permission isolation | VFS boundaries inside container + cgroup limits | `internal/runtime/permission/`, `internal/runtime/vfs/`, `internal/container/` |
| Test-driven feedback | Existing test runner tool | `internal/tools/` |

## What's net-new

| Component | Responsibility | Rough size |
|---|---|---|
| **Orchestrator** | Decompose goal → task queue; spawn workers; track lifecycle | Small — wrapper around existing parallel-agents + an LLM call |
| **Task queue** | Persistent (SQLite already in pkg/memex/store), assignment policy, retry-on-fail | Small — straight CRUD |
| **Worker harness** | Per-agent container provisioning (image, mounts, cgroups, network), branch+worktree, session/memory bind, auto-PR on completion | Medium — composes existing podman + git + agent tools, new lifecycle |
| **Integrator agent** | LLM judge over PRs: pass tests, lint, sanity-check intent vs. diff; sequence merges | Medium — reuses `/review` skill machinery |
| **Conflict policy** | Auto-rebase before merge; fall back to "kick to worker" or "ask human" | Small but tricky semantics |
| **CLI surface** | `ycode swarm <goal>`, `ycode swarm --workers N < tasks.txt`, `ycode swarm status` | Small |
| **Gitea ↔ container plumbing** | How containers reach the embedded Gitea (port forward, shared socket, or each container bridged into the host network) | Small — a config decision, not new code |
| **Observability** | Web UI panel (Phase 6 portal) showing in-flight agents + their branches + PR statuses + container resource usage | Big — Phase 6 work, not blocking v0.1 |

## What containerization buys us (the load-bearing argument)

Without podman, multi-agent on shared host has subtle failure modes:
- Agent A's `npm install` clobbers shared `node_modules` for Agent B
- Agent A reads Agent B's API keys from `~/.env`
- Agent A spawns runaway processes that consume host CPU
- A bad agent `rm -rf`s the workspace
- Agents trip over each other's port reservations (LSP, dev servers, etc.)

With embedded podman per agent:
- **Filesystem isolation**: each container has its own root; only the per-agent worktree is bind-mounted in
- **Network isolation**: each container has its own network namespace; egress can be restricted (e.g., only allow embedded Gitea + the chosen LLM provider's API)
- **Resource caps**: cgroups enforce `--cpus`, `--memory`, IO caps — one runaway agent can't starve the others
- **Secret isolation**: only the credentials the agent needs are passed in; host env vars don't leak
- **Disk cost**: overlay FS shares the base sandbox image across all agents (~hundreds of MB once, not per-agent); incremental cost per agent is just its worktree
- **Clean teardown**: `podman rm` ends all of an agent's processes atomically — no orphan tail-logs, no stuck dev servers
- **Independent build/test cycles** *(load-bearing)*: each agent has its own build cache, its own /tmp, its own port range, its own process tree. Agent A's `make build` doesn't fight Agent B's `go test` for `go/pkg/mod` locks; Agent C can bind a dev server on :8080 while Agent D also wants :8080 (each container has its own network namespace so they're not the same :8080). Without this, multi-agent on the same repo collapses into "one agent at a time, please" the moment any task does anything compile-heavy or runs a dev server. Containers are what make truly parallel edit→compile→test loops actually work.

This is the same isolation story Cursor / Cline / Aider have to outsource to Docker Desktop or claim "trust me." ycode does it built-in.

## Coordination protocol — minimum viable

- **Assignment**: pull-based. Workers idle until they pull a task from the queue. No push-based scheduling complexity.
- **Conflict surface**: only at PR-merge time. No mid-flight cross-agent locking.
- **Done signal**: worker opens PR with test-pass evidence + a one-line "what I changed and why." Integrator decides merge.
- **Failure modes**:
  - Worker exceeds budget/timeout → marks task "needs help," queue requeues with note
  - PR conflicts → integrator kicks back to worker for rebase; worker has 2 retries; then human
  - Tests fail in worker's branch → worker fixes-or-marks-blocked; PR doesn't open until green
- **Observability**: structured events on the existing OTEL pipeline so each step is traceable post-hoc.

## Phasing

**v0.1 — Embarrassingly parallel** (smallest useful)
- `ycode swarm < tasks.txt` runs N workers, each takes one line as its task description, branch-per-agent, PRs back, no integration agent (PR list is the handoff to a human).
- All existing pieces; no orchestrator, no decomposition, no integrator. Ships in days.

**v0.2 — Plan-then-divide**
- Orchestrator LLM decomposes a single goal into tasks before queuing.
- Adds a real integrator that judges PRs + sequences merges.

**v0.3 — Mesh + pipeline mode**
- Graduate mesh from `experimental` to `stable`.
- Add pipeline orchestration (researcher → coder → reviewer chains).

**v0.4 — Portal observation surface**
- Web UI panel showing live workers, branches, PR statuses (Phase 6 portal pillar).

Each step ships independently and is genuinely useful on its own.

## Open questions

1. **Decomposition quality**: how much does outcome depend on the orchestrator LLM splitting tasks well? May need eval harness for "good decomposition" early.
2. ~~Worktree disk cost~~ — *resolved by podman: overlay FS, base image shared across agents.*
3. **Token budget**: N concurrent agents × LLM context = expensive. Per-swarm budget cap (`--budget` from Phase 3)?
4. ~~Resource limits~~ — *resolved by podman: cgroups (`--cpus`, `--memory`) per container. Default `--workers` cap based on detected host resources; user can override.*
5. **Cross-agent memory**: should workers share episodic memory (see each other's progress) or be isolated? Default isolated, opt-in shared?
6. **When to kick to human**: clear escalation criteria — number of failed retries, ambiguity in conflict, etc.
7. **Auth model for inner Gitea**: every agent (running in a container) needs to reach the embedded Gitea on the host. Per-container service tokens minted by the orchestrator at spawn? Tokens scoped to the agent's branch namespace only? How does the container reach Gitea — port-forward, shared socket, or container-network bridge?
8. **Egress policy**: by default, should worker containers be allowed internet access (for LLM API calls) or only intra-host traffic to Gitea + the orchestrator's LLM proxy? Internet-allowed is simpler; restricted is the wedge story (offline-mode multi-agent).
9. **Container image**: do we ship a `ycode-worker:latest` image as part of the binary (embed and load on first run), or build it on demand from a Dockerfile? First run latency vs. binary size tradeoff.

## Strategic placement

The embedded-podman insight upgrades my own initial Phase 4 placement. Three honest options:

- **Phase 4 differentiator** (original): a great feature once Phase 1–3 are done. Subsumes "speculative parallelism" from the existing Phase 4 list.
- **Wedge-strengthening (Phase 2 promotion candidate)**: "fully-isolated team-of-agents on a single binary, runs offline" is genuinely something no popular competitor can claim. If the offline-mode SWE-bench in Phase 1 is proof of "one agent can run offline," this would be proof of "a *team* can run offline." That's a Phase-1-level marketing artifact.
- **Portal pillar 5** (`Code, ops, comms, knowledge, **team**`): the daily-driver pitch becomes "your team-of-agents lives in your repo, runs in your sandbox, talks to your local Gitea — never leaves your machine." Strongest narrative version.

**My recommendation**: position as **Phase 4 anchor**, but with explicit framing as wedge-strengthening so the marketing payoff (and the offline-mode-team benchmark) doesn't get lost. If we hit Phase 4 and the offline angle is going viral on benchmarks, promote forward.

## Decision needed before implementation

1. **Scope**: do we ship v0.1 (embarrassingly parallel + container-per-agent — days of work) or wait until v0.2 (orchestrator + integrator — week+)?
2. **Mesh graduation**: does this work require graduating `mesh-auto-start` from `experimental` to `stable`? Probably yes by v0.3.
3. **Budget cap**: should the cost-meter Phase 3 lever land first? Multi-agent burns money fast; meter+budget make it safe.
4. **Phase placement**: confirm Phase 4 anchor vs. promotion. The embedded-podman + embedded-Gitea combination is genuinely unique — promotion to a Phase 1/2 marketing artifact (offline-mode team-of-agents benchmark) is a real possibility.
5. **Worker image strategy**: embed and lazy-load (`ycode-worker:latest` as part of the binary, +disk on first run) vs. on-demand build from Dockerfile (smaller binary, slower first run).

Open the discussion on these five. Once decided, I'll update `docs/strategy.md` and we can scope an implementation plan.

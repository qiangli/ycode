---
topic: loom
summary: isolated git workspaces for parallel sub-agent work
when: you need to fan out work without sub-agents stepping on each other
audience: agent
max_lines: 120
---

Loom hands out sandboxed git workspaces — each one a fresh clone on
its own branch with its own author identity — so several sub-agents
(or several attempts at the same task) can run in parallel without
fighting over the same working tree.

## When to use this

- You're launching multiple agents against the same repo and they'll
  each be editing files. Without loom they share one cwd and clobber
  each other.
- You want a clean "try, push, PR, release" lifecycle that's easy to
  roll back. Each lease is independently abortable via `loom_release`.
- A "merge wars" failure mode would be unacceptable — e.g. autopilot,
  collab, sprint, mesh runs.

Loom is HTTP-MCP only (requires `ycode serve` running). It is NOT
available on the stdio transport.

## Lifecycle

```
loom_lease   →  cwd = <sandboxRoot>/<id>, branch = agent/agent-loom-<sub_agent_label>-<id8>/free-<rand>
  ⇣ (sub-agent does work in that cwd)
loom_push    →  stages + commits + pushes branch upstream
  ⇣
loom_merge   →  opens PR (or returns existing PR number)
  ⇣ (merger handles auto-merge once CI is green)
loom_release →  tears down sandbox; safe iff no PR still open
```

`<sandboxRoot>` is the manifest's `loom.sandboxRoot` (typically
`~/.agents/ycode/gitea/loom/sandboxes/`); the branch pattern is the
manifest's `loom.branchNamePattern`.

States returned by `loom_status`: `leased | pushed | merging | merged
| ci_failed | conflict`. You can call `loom_status` at any point to
check where a lease is — pass `loom_id` for one lease, `cwd` for all
leases in a project, or no args for everything.

## Tool calls

- **`loom_lease`** — Reserve a workspace. **Required args:** `cwd`
  (absolute project path) and `sub_agent_label` (short identifier;
  becomes part of the branch name and the git author trailer).
  **Optional args:** `ttl_seconds` (default 3600, max 28800) and
  `base_branch` (default `main`). Returns `{loom_id, sandbox_path,
  branch}`. The sub-agent should `cd` into `sandbox_path` and do its
  work there.
- **`loom_push`** — Stage every change in the sandbox, commit (using
  the lease's author identity), and push. Idempotent: no-op commit
  when nothing changed; existing HEAD is still pushed. Optional:
  `message` (default `loom: <sub_agent_label>`) and `force` (boolean;
  allow non-fast-forward push).
- **`loom_merge`** — Open a PR into main. Idempotent: if a PR is
  already open for the branch, returns its number rather than
  re-opening. The merger watches CI and auto-merges when green.
  Optional: `title` and `body` PR fields.
- **`loom_status`** — Inspect lease state. Argless form lists every
  active lease.
- **`loom_release`** — Tear down. By default removes the sandbox AND
  deletes the branch, but only if no PR is still open. Open PRs are
  left alone so the merger can finish.

## Failure modes

| Symptom | Fix |
|---|---|
| `loom_lease` returns "no project" | `ycode serve` is up but loom subsystem isn't configured for this cwd; check serve logs. |
| `loom_push` fails with "dirty submodule" | The sandbox has a submodule the agent did not commit; commit it explicitly or release + retry. |
| `loom_merge` returns same PR twice | This is the idempotent contract, not a bug — only one PR exists per branch. |
| `loom_release` refuses with "PR still open" | Expected — let the merger finish, or close the PR manually first. |
| Stdio agent can't see `loom_*` | Loom is HTTP-only; switch to the HTTP MCP transport (start `ycode serve`, run `ycode pair --tool <client>`). |

## Exact calls

- Start the HTTP MCP server: `ycode serve`
- Wire a client to it: `ycode pair --tool claude-code` (or other)
- Lease a workspace: MCP `loom_lease` with `{cwd, sub_agent_label, ttl_seconds?, base_branch?}`
- Push work: MCP `loom_push` with `{loom_id}`
- Open PR: MCP `loom_merge` with `{loom_id}`
- Status of one lease: MCP `loom_status` with `{loom_id}`
- All leases in a project: MCP `loom_status` with `{cwd}`
- Tear down: MCP `loom_release` with `{loom_id}`

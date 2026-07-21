---
topic: loom
summary: isolated git workspaces — moved out of ycode to weave
when: you need to fan out work without sub-agents stepping on each other
audience: agent
max_lines: 120
---

Loom was ycode's lease/push/merge/release substrate for handing out
sandboxed git workspaces — one fresh clone per sub-agent, each on its
own branch with its own author identity — so parallel agents never
clobbered a shared working tree.

**It is no longer part of ycode.** The `loom_*` verbs were reachable
only over the MCP composite endpoint, and MCP has been removed: ycode
neither exposes nor consumes MCP, `ycode serve` mounts no `/mcp/`
route, and there is no in-session or `yc` replacement for `loom_lease`,
`loom_push`, `loom_merge`, `loom_status`, or `loom_release`. If your
prompt or tool list still mentions them, it is stale — do not try to
call them and do not ask the user to start a server for them.

The surviving surface for the same problem is the sibling AgentOS
shell command **`bashy weave`**: a local, filesystem-based orchestrator
that runs agentic CLIs in parallel over one repo, each in an isolated
git-clone workspace, then converges the work back. No server, no forge,
no MCP.

## When to use this

- You're launching multiple agents against the same repo and they'll
  each be editing files. Without isolation they share one cwd and
  clobber each other.
- You want a clean "try, verify, merge" lifecycle that's easy to roll
  back — each run is independently abandonable.
- A "merge wars" failure mode would be unacceptable.

Read the full playbook with `bashy weave guide` before driving it; the
summary below is only enough to get oriented.

## Lifecycle

```
bashy weave add "<title>"      →  issue lands in the repo-local queue
  ⇣
bashy weave start -- <tool>    →  claims an issue, allocates an isolated
                                  git-clone workspace, launches the tool
  ⇣ (sub-agent does its work in that workspace)
bashy weave list               →  runs in flight (TOOL / STARTED / DUR)
bashy weave log <N> [-f]       →  live capture of one run
  ⇣
bashy weave pull               →  absorb converged work back into the repo
```

`bashy weave abandon <N>` tears a run down (workspace, branch, and any
running wrapper). `bashy weave check` lists every subcommand and its
implementation status.

## What ycode still owns

- The **agent runtime** each weave run launches (`ycode` itself, or any
  other CLI — weave is tool-agnostic).
- The `yc <verb>` built-ins the sub-agent uses inside its workspace:
  `yc symbols`, `yc search-symbols`, `yc refs`, `yc repomap`,
  `yc git`, `yc test`, `yc run`.

ycode does **not** own workspace leasing, branch allocation, PR
creation, or merge arbitration any more. Route those to weave.

## Failure modes

| Symptom | Fix |
|---|---|
| Your tool list advertises `loom_*` | Stale prompt. Those tools do not exist; use `bashy weave`. |
| `ycode serve` is running but no loom endpoint | Correct — `serve` mounts no MCP route at all. |
| `bashy: command not found` | The AgentOS shell isn't installed on this host. There is no ycode-side fallback; tell the user. |
| Two agents editing the same tree | You skipped workspace isolation. Stop, `bashy weave add` the work, and fan out properly. |

## Exact calls

- Read the playbook: `bashy weave guide`
- File a unit of work: `bashy weave add "fix null deref in cache"`
- Fan a tool out onto it: `bashy weave start -- codex "<body>"`
- See what's in flight: `bashy weave list`
- Follow one run's output: `bashy weave log <N> -f`
- Absorb finished work: `bashy weave pull`
- Tear a run down: `bashy weave abandon <N>`

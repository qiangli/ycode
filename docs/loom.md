# Loom — removed; use `bashy weave`

> **Status: removed from ycode (2026-07).** Loom's five verbs
> (`loom_lease` / `loom_push` / `loom_merge` / `loom_status` /
> `loom_release`) were reachable only over the MCP composite endpoint.
> MCP has been removed in full — `ycode serve` mounts no `/mcp/` route,
> there is no `ycode mcp` subcommand, and `pkg/loom` /
> `internal/gitserver/` no longer exist in this tree. There is no `yc`
> verb, in-session tool, or HTTP route that replaces them. See
> [plan-remove-mcp.md](./plan-remove-mcp.md).

Loom handed each of a foreign tool's sub-agents an isolated git
workspace — clone, branch, author identity — so N parallel sub-agents
could attack the same repo without stepping on each other, converging
through a Gitea-backed merger/CI gate.

## What to use instead

The same problem is solved by the sibling AgentOS command **`bashy
weave`**: a local, filesystem-based orchestrator that runs agentic CLIs
in parallel over one repo, each in an isolated git-clone workspace, then
pulls the converged work back. No server, no forge, no MCP.

```bash
bashy weave guide                        # the playbook — read this first
bashy weave add "fix null deref"         # seed an issue into the queue
bashy weave start -- codex "<body>"      # claim, allocate workspace, launch
bashy weave list                         # runs in flight
bashy weave log <N> -f                   # live capture
bashy weave pull                         # absorb converged work
bashy weave abandon <N>                  # tear a run down
```

The agent-facing version of this note is `ycode docs loom`.

## Historical material

The v1 design above and the v2 successor design are kept as records of
past analysis, not as setup instructions:

- [`loom-v2-plan.md`](./loom-v2-plan.md)
- [`loom-v2-implementation.md`](./loom-v2-implementation.md)

If you are reading either for a build task, note that the ycode-side
implementation they describe has been deleted.

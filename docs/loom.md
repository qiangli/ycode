# Loom — git-workspace substrate for foreign agentic tools

`loom` is one of ycode's local-first services exposed to third-party
agentic coding tools (Claude Code, OpenCode, Codex, Gemini CLI),
alongside `podman` (sandbox), `ollama` (local inference), `otel`
(observability), and `memex` (memory + graph).

It hands each of a foreign tool's sub-agents an isolated git workspace
— clone, branch, author identity — so N parallel sub-agents can attack
the same repo without stepping on each other. Convergence happens
through ycode's existing merger/CI gate, which auto-merges PRs once CI
is green.

ycode is **infrastructure** here; the foreign tool keeps running its
own agent loop. ycode does not call the foreign tool's LLM, dispatch
its prompts, or pick its tasks.

## Five verbs

| Verb | Input | Output |
|------|-------|--------|
| `loom_lease` | `{cwd, sub_agent_label, ttl_seconds?, base_branch?}` | `{loom_id, path, branch, clone_url, author_name, author_email, expires_at}` |
| `loom_push` | `{loom_id, message?, force?}` | `{commit_sha, branch, pushed}` |
| `loom_merge` | `{loom_id, title?, body?}` | `{pr_number, status:"queued"}` |
| `loom_status` | `{loom_id?, cwd?}` | `[{loom_id, branch, state, pr_number?, ci_tail?}]` |
| `loom_release` | `{loom_id, keep_branch?}` | `{released:true}` |

Foreign tools never touch Gitea concepts (repo names, owners, PR
numbers). The opaque `loom_id` handle is round-tripped on every call.

State values returned by `loom_status`: `leased`, `pushed`, `merging`,
`merged`, `ci_failed`, `conflict`.

## Quickstart for foreign tools

Run any ycode entry point (e.g. `ycode init` or just `ycode`) inside any git repo — selfinit detects installed agentic tools and registers ycode's MCP servers + instructions automatically. Restart the foreign tool to pick them up. See [`docs/selfinit.md`](./selfinit.md) for the full mechanism.

To inspect what would be registered without writing:

```
ycode init --doctor
```

To force a refresh after manifest changes:

```
ycode init --refresh
```

## Discovery

`ycode serve` writes `~/.agents/ycode/manifest.json` (`schemaVersion=4`)
with a top-level `loom` block. As of schemaVersion 4 the loom MCP handler
is exposed via the composite `/mcp/` endpoint (the per-family `/loom-mcp/`
route was retired):

```json
{
  "loom": {
    "mcp": "http://127.0.0.1:PORT/mcp/",
    "leaseTTLDefaultSeconds": 3600,
    "leaseTTLMaxSeconds": 28800,
    "subAgentIdentityConvention": "agent-loom-<label>",
    "cloneURLTemplate": "http://127.0.0.1:PORT/git/admin/{slug}.git",
    "tokenFile": "~/.agents/ycode/gitea/admin.token",
    "sandboxRoot": "~/.agents/ycode/gitea/loom/sandboxes",
    "branchNamePattern": "agent/agent-loom-<label>-<id8>/free-<rand>"
  },
  "mcp": {
    "http": {
      "ycode": "http://127.0.0.1:PORT/mcp/"
    }
  }
}
```

The foreign tool's bootstrap reads this once, registers `loom.mcp` as
an HTTP MCP server in its own session, and is done.

`.mcp.json` at the repo root remains the stdio entry for ycode's
read-only capabilities (treesitter AST search). Loom's mutating verbs
require write permission, so they ride HTTP MCP under `ycode serve`,
where the prompting gate can authorize writes.

## Identity & auth

Single admin Gitea user, single admin token at
`~/.agents/ycode/gitea/admin.token`. Per-sub-agent identity rides in:

- branch name: `agent/agent-loom-<label>-<id8>/free-<rand>`
- git author trailer: `agent-loom-<label>-<id8> <agent-loom-<label>-<id8>@ycode.local>`

The `agent-loom-` prefix in branches and author names makes it trivial
to filter foreign-driven work in OTel logs and `git log` from ycode's
own internal collab work, without provisioning per-sub-agent Gitea
users. The separator MUST stay ref-safe: `git check-ref-format` rejects
`:` (along with `?`, `^`, `~`, `\`, `*`, and whitespace) in branch
names, which is why we use `-` rather than `:`.

## Lifecycle

- Default TTL: **1 hour** (`leaseTTLDefaultSeconds`); cap **8 hours**.
- Idle timeout: **30 minutes** since last `loom_*` call referencing the
  lease.
- Reaper: runs every 60s. Expired leases with no open PR are torn down
  (sandbox + branch); leases with an open PR have their sandbox
  reclaimed but the branch left for the merger.
- A foreign tool that crashes without calling `loom_release` is
  cleaned up by the reaper within the idle window.
- `ycode serve` restart runs one immediate reaper pass on startup.

## Worked example: Claude Code spawns 3 sub-agents

1. Parent reads `~/.agents/ycode/manifest.json`, finds `loom.mcp =
   http://127.0.0.1:PORT/mcp/`, registers it as an HTTP MCP server
   in its own session.
2. Parent splits a refactor 3 ways (`extract-types`, `migrate-callers`,
   `update-tests`) and calls `loom_lease` × 3 with distinct
   `sub_agent_label`s. Returns 3 sandbox paths.
3. Parent spawns 3 sub-agents in its own loop (its LLMs, its tools).
   Each sub-agent works in `cwd=<lease.path>`. They never see each
   other.
4. As each sub-agent finishes, parent calls `loom_push` then
   `loom_merge`. PRs open against `main`.
5. The merger ticks every 10s, runs CI per PR in a temp worktree, merges
   on green via Gitea's API.
6. Parent polls `loom_status`; states transition `pushed → merging →
   merged`. Conflicts surface as `conflict` — parent hands them back to
   its own sub-agent for a rebase.
7. Parent calls `loom_release` × 3.

The user's host repo is never touched by loom. They pull converged
`main` via `ycode tasks pull` when ready.

## Public Go API

```go
import "github.com/qiangli/ycode/pkg/loom"

svc, err := loom.NewService(loom.Options{
    Backend:     myBackend,             // implement loom.Backend
    SandboxRoot: "/var/loom/sandboxes",
})
defer svc.Close()

lease, _ := svc.Lease(ctx, loom.LeaseRequest{
    CWD: "/path/to/repo", SubAgentLabel: "extract-types",
})
// ... sub-agent works in lease.Path ...
svc.Push(ctx, loom.PushRequest{LoomID: lease.ID})
svc.Merge(ctx, loom.MergeRequest{LoomID: lease.ID})
svc.Release(ctx, loom.ReleaseRequest{LoomID: lease.ID})
```

The default ycode-bundled implementation is gitea-backed (under
`internal/gitserver/loom`); external Go consumers can supply their own
`Backend` to target a different forge.

## Out of scope (v1)

- Per-Gitea-user identity for foreign sub-agents. Branch + author
  trailer is sufficient.
- SSH transport. HTTP-with-token works and avoids key management.
- Remote network exposure. 127.0.0.1 only.
- Cross-project leases (one lease spanning multiple `cwd`s).
- Resource quotas (max sandboxes per foreign tool, disk caps).
- Custom CI per-lease. CI is per-project (existing
  `merger.Config.CICommand`).
- Conflict auto-resolution. Surface conflicts via `loom_status`; the
  foreign tool's LLM resolves.
- Rich PR review tools. Already covered by the gitea family on `/mcp/`
  endpoint.

# `ycode weave` runbook — three agents, three issues, end to end

Worked example: one human user, three issues, three different agentic CLIs, parallel execution, converged result pushed to GitHub. Use this as the canonical walk-through when teaching the feature or testing it end-to-end.

See [`loom-v2-plan.md`](./loom-v2-plan.md) for the design rationale behind every step below.

## Scenario

Alice has cloned `github.com/alice/myapp` locally to `~/projects/myapp/`. She has three pieces of work — fix issue #123, refactor for #124, add the feature in #125 — and wants Codex on #123, OpenCode on #124, and Claude Code on #125 to work them in parallel. End state: three commits sitting on her local `main`, ready to push to GitHub.

This runbook assumes ycode is installed (`brew install ycode` or equivalent) and the three agent CLIs (`codex`, `opencode`, `claude-code`) are on `$PATH`.

## Phase 0 — One-time setup (~30 seconds)

### 0.1 Start `ycode serve`

`weave` needs the embedded Gitea + merger running. Either run `ycode serve` in a background terminal or let it auto-start on first `weave` call. For this runbook:

```bash
cd ~/projects/myapp
ycode serve --background
```

Expected output:

```
ycode serve: starting on 127.0.0.1:5743
manifest: ~/.agents/ycode/manifest.json
gitea:    http://127.0.0.1:5743/gitea/
ready.
```

Verify health:

```bash
ycode serve status
# → ok (gitea, merger, mcp all green)
```

### 0.2 (Optional) selfinit your agent tools

If you haven't yet, run:

```bash
ycode init
```

This registers ycode's MCP servers and the `weave`-aware launch hooks into `~/.config/claude-code/`, `~/.config/codex/`, `~/.config/opencode/`. Each tool, after restart, knows to behave correctly under auto-attach (refuse to run unmanaged, read `YCODE_LOOM_ISSUE_BODY`, etc.). Skip this step if your tools don't support MCP — `weave` still works, you'll just feed prompts manually in Phase 2.

## Phase 1 — First-run mirror & label setup (one-time, ~3 seconds)

The first `ycode weave start` in this repo bootstraps everything. You don't run a separate setup command; it just happens. You'll see:

```bash
ycode weave start --issue 123 --title "fix null deref in cache" -- codex
```

Expected output (first run only):

```
weave: first run in this project — setting up
  ✓ mirror   admin/myapp into local Gitea
  ✓ labels   loom:working, loom:submitted, loom:ci-failed, loom:conflict, loom:merged, loom:abandoned
  ✓ board    Loom (6 columns)
  ✓ hook     pre-commit installed in .git/hooks/
  ✓ config   wrote .ycode/loom.yaml (backend: forge, identity: ephemeral)
weave: issue #123 created in local Gitea
weave: sandbox at ~/.agents/ycode/gitea/loom/sandboxes/ab12cd34/
weave: launching codex...
```

Codex starts inside the sandbox. From here on, every subsequent `weave start` skips the setup block and lands sub-second.

If you want to inspect the Gitea board now:

```bash
ycode weave open --board
# opens http://127.0.0.1:5743/gitea/admin/myapp/projects/1 in your browser
```

You'll see a kanban with `working / submitted / ci_failed / conflict / merged / abandoned` columns and one card for issue #123.

## Phase 2 — Start the other two weaves

Open two more terminals. One command each.

### Terminal 2 — OpenCode on #124

```bash
cd ~/projects/myapp
ycode weave start --issue 124 --title "refactor user service for testability" -- opencode
```

Expected output:

```
weave: issue #124 created in local Gitea
weave: sandbox at ~/.agents/ycode/gitea/loom/sandboxes/ef56gh78/
weave: launching opencode...
```

OpenCode starts.

### Terminal 3 — Claude Code on #125

```bash
cd ~/projects/myapp
ycode weave start --issue 125 --title "add dark mode toggle" -- claude-code
```

```
weave: issue #125 created in local Gitea
weave: sandbox at ~/.agents/ycode/gitea/loom/sandboxes/ij90kl12/
weave: launching claude-code...
```

Three agents now run in three isolated sandboxes. None can see the others' files, branches, stashes, hooks, or in-progress commits (per the sandbox-isolation invariant in the plan doc).

### What each agent sees

Inside its sandbox, each tool has:

- `cwd` = the sandbox path (a full `git clone --reference` of myapp).
- Environment:
  - `YCODE_LOOM_ID=loom-...`
  - `YCODE_LOOM_BRANCH=agent/agent-loom-issue-123-.../free-...`
  - `YCODE_LOOM_BASE=main`
  - `YCODE_LOOM_ISSUE=123`
  - `YCODE_LOOM_ISSUE_TITLE="fix null deref in cache"`
  - `YCODE_LOOM_ISSUE_BODY="<the issue body>"`
- MCP registry filtered to the **sub-agent role**: `loom_checkpoint`, `loom_submit`, `loom_abandon`, plus the `loom://session` resource. The agent does not see `loom_open` or any of the parent verbs.

If the tool is selfinit-registered, it auto-reads `YCODE_LOOM_ISSUE_BODY` and starts working. If not, you'll need to give it a starter prompt — the title and body are right there in env vars:

```
codex> work on the issue described in $YCODE_LOOM_ISSUE_BODY; the title is "$YCODE_LOOM_ISSUE_TITLE"
```

## Phase 3 — Monitor (passive, optional)

You can let the agents run untouched. To peek at progress, two options.

### Option A — Terminal: `ycode weave list`

In any fourth terminal:

```bash
ycode weave list --watch
```

Live-updating TUI:

```
ISSUE  TOOL          STATE          SANDBOX                       HEARTBEAT  ACTION
#123   codex         working        .../sandboxes/ab12cd34        2s         editing
#124   opencode      submitted      .../sandboxes/ef56gh78        4m         CI running
#125   claude-code   working        .../sandboxes/ij90kl12        1s         editing
```

`--watch` streams state transitions over the `loom://project` MCP resource — no polling.

### Option B — Browser: Gitea project board

```bash
ycode weave open --board
```

The kanban shows three cards moving through columns as state transitions fire. Click any card → standard Gitea issue page with comments, PR link, CI status, sticky loom comment showing sandbox path and heartbeat.

For programmatic monitoring (e.g., from a script or higher-level orchestrator):

```bash
ycode weave list --watch --json | jq -c '. | {issue: .result.issue, from: .result.from, to: .result.to}'
# {"issue":124,"from":"working","to":"submitted"}
# {"issue":124,"from":"submitted","to":"merged"}
# ...
```

## Phase 4 — Convergence (autonomous)

As each agent finishes its work, it calls `loom_submit`. (If a tool exits cleanly without calling submit, the wrap layer auto-submits its sandbox state — selfinit-installed handler.)

What happens behind the scenes for each:

1. Branch pushed to local Gitea.
2. PR opened against `main`. Card moves to `submitted` column.
3. Merger picks up the PR, rebases onto current `main`.
4. CI runs (whatever's in `.ycode/loom.yaml` `ci_command`, or auto-detected from your project — `make test`, `npm test`, etc.).
5. Green CI → fast-forward merge into local Gitea's `main`. Card moves to `merged`. Issue auto-closes via `Fixes #N` trailer.

The first PR in is trivial; subsequent PRs may need a rebase (handled silently if no conflict) or surface a `conflict` state for the originating agent to resolve in-place.

### What if there's a conflict?

Suppose #123 and #124 both touch `internal/cache/cache.go`. The merger:

1. Merges #123 first (it arrived first).
2. Tries to rebase #124 → conflict.
3. Rebases #124's branch onto new `main` in the sandbox, leaves conflict markers in `cache.go`.
4. PR card moves to `conflict` column.
5. `loom_submit` returns `{state: conflict, files: ["internal/cache/cache.go"]}` to the agent.
6. Agent edits the conflicted file using its normal tools, calls `loom_submit` again.
7. Loop until green.

If the agent already exited (some tools stop after a `loom_submit`), you can reattach:

```bash
ycode weave start --resume --issue 124 -- opencode
```

This drops a fresh OpenCode session into the same sandbox, where the conflict is sitting ready to resolve.

If you want to fix it yourself:

```bash
ycode weave shell 124
# drops into a shell already in the sandbox with author identity active
$ git status
# you see the conflict
$ $EDITOR internal/cache/cache.go
$ git add internal/cache/cache.go
$ ycode weave submit 124         # CLI shortcut for loom_submit
```

### What if CI fails?

Card moves to `ci_failed`. The merger posts a comment on the PR with the failing job name and last 200 lines of output. Same recovery model: the agent (or you, via `weave shell`) fixes and re-submits.

## Phase 5 — Pull converged work back

Your original `~/projects/myapp/` checkout has been completely untouched throughout — that's Guarantee 1 of the design. The merger has been advancing the local Gitea's `main` as each PR merged. Now you absorb that work.

### 5.1 Check state

```bash
ycode weave list
```

```
ISSUE  TOOL          STATE     PR           MERGED AT
#123   codex         merged    !1           2 min ago
#124   opencode      merged    !2           1 min ago
#125   claude-code   merged    !3           30s ago
```

All green. (If anything is still `working` or `conflict`, address it before pulling.)

### 5.2 Fast-forward your local main

```bash
cd ~/projects/myapp
ycode weave pull
```

Expected output:

```
weave pull: stashing 0 uncommitted changes
weave pull: fast-forward main from a3f9b2 to 8c41e5
  + 8c41e5 fix null deref in cache (#123)
  + 7d22f1 refactor user service for testability (#124)
  + 2b94a8 add dark mode toggle (#125)
weave pull: 3 commits absorbed, 0 conflicts (always — pull is always FF)
weave pull: re-applying stash (none)
```

If you'd had uncommitted edits in your checkout, they'd be stashed before the FF and re-applied after. If you'd had *committed* local edits diverging from Gitea's main, `weave pull` would refuse and tell you to either rebase or merge manually — it never silently rewrites your local commits.

### 5.3 Verify

```bash
git log --oneline -5
```

```
8c41e5 (HEAD -> main) fix null deref in cache (#123)
7d22f1 refactor user service for testability (#124)
2b94a8 add dark mode toggle (#125)
a3f9b2 <previous commit>
...
```

Three commits, properly attributed (committers are `agent-loom-issue-123-...@ycode.local` etc.), all clean, all on `main`. Each commit's body contains `Fixes #N` for the local Gitea issue plus a footer linking the original sandbox lease and the merge PR for traceability.

## Phase 6 — Push to GitHub

Standard git from here. Nothing weave-specific.

```bash
git push origin main
```

Expected:

```
To github.com:alice/myapp.git
   a3f9b2..8c41e5  main -> main
```

Done. Three issues' worth of work, three different agents, one push.

## Phase 7 — Cleanup (mostly automatic)

The reaper handles most of it:

- Merged leases are torn down within the grace window. Sandbox directories removed. Branches preserved in Gitea for audit (deletable later via `ycode loom prune` if you care about disk).
- The Loom project board keeps the cards in `merged` column as history; they auto-archive after 30 days.

If you want to nuke everything for this project explicitly:

```bash
ycode weave reset
# Confirm: remove 3 leases, 3 branches, 0 sandboxes (all reaped), preserve labels and project board? [y/N]
```

## Common situations cheat sheet

### Tool crashed mid-work

Wrap detected the exit. Sandbox is intact for 30 minutes idle grace. Re-attach:

```bash
ycode weave start --resume --issue 124 -- opencode
```

The same sandbox, same branch, same author identity. Whatever the tool had committed locally is still there.

### You want to take over manually

```bash
ycode weave shell 124
```

Drops into a shell inside the sandbox with the lease's author identity already configured in `git config user.email`. Edit, commit, submit:

```bash
$ vim ...
$ git add -p
$ git commit -m "rebalance under load"
$ ycode weave submit 124
$ exit                            # back to your normal shell
```

### One agent is stuck / hallucinating

Kill it cleanly:

```bash
ycode weave abandon 124 --reason "going to redo with different prompt"
```

Sandbox removed, branch removed (since no PR open), lease closed, card moves to `abandoned` column in Gitea. Start fresh:

```bash
ycode weave start --issue 124 -- codex      # try a different tool
```

### You want to merge #125's PR before #124's

By default the merger goes in arrival order. To prioritize:

```bash
ycode weave open --issue 125         # opens the PR in Gitea
# In Gitea, click "Merge now" — bypasses the queue
```

Or via CLI:

```bash
ycode loom merge-now --pr 3          # substrate-admin command
```

### You want to review a PR before it merges

By default the merger auto-merges on green CI. To require manual approval, set in `.ycode/loom.yaml`:

```yaml
merger:
  approval_required: true
```

PRs then wait in `submitted` state until you approve them in Gitea (or via `ycode loom approve --pr N`).

### You want to push intermediate work to GitHub before all three are done

Nothing stops you. After Phase 5.2 (whether one PR or three have merged), `git push origin main` works normally. The local Gitea is just a staging area; GitHub is the truth-source.

### You closed all three terminals and walked away

Heartbeats stopped. After 30 min idle (default), the reaper:

- Tears down sandboxes whose leases have no open PR.
- Leaves sandboxes whose leases have an open PR alone (merger may still need them).
- Preserves all branches in Gitea.

When you come back: `ycode weave list` shows what's still active. `ycode weave list --history` shows what was reaped. Lost work surface = zero (everything that was committed in the sandbox is preserved as a Gitea branch).

## What you never had to do

- Configure CI for the local Gitea (auto-detected from your project files).
- Set up per-agent identity, tokens, or branch naming conventions.
- Choose a backend mode (auto-selected: `forge` since you had `npm test`).
- Manage sandbox lifetimes or TTLs.
- Coordinate the three agents to avoid stepping on each other.
- Resolve a merge conflict in your working tree (the merger handles it in the sandboxes).
- Touch the Gitea UI for anything other than glancing at the board.

The contract was: three terminals, one command per, walk away, pull, push. Everything else is the substrate's problem.

## Programmatic variant — same workflow, agent-driven

If you're an orchestrator agent instead of a human at a keyboard, the same workflow looks like this (using the agent-friendly CLI conventions from the plan doc):

```bash
export YCODE_AGENT=1     # forces --json, no prompts, no colors

# Phase 1+2 — start three weaves and capture the loom_ids
for issue in 123 124 125; do
  ycode weave start --issue $issue --tool codex --no-spawn --json \
    | jq -r '.result.loom_id'
done

# Then spawn each tool yourself via loom_handoff (MCP) or shell-out,
# attached to the respective YCODE_LOOM_ID.

# Phase 3 — watch transitions as NDJSON
ycode weave list --watch --json &
WATCH_PID=$!

# ... do other work ...

# Phase 4 wait — block until all three are terminal
ycode weave wait --issue 123 --issue 124 --issue 125 --timeout 1h --json

# Phase 5 — pull
ycode weave pull --json

kill $WATCH_PID
```

Every command returns a versioned envelope; exit codes distinguish success / config error / state conflict / unhealthy dep. See [Agent-friendly CLI](./loom-v2-plan.md#agent-friendly-cli) in the plan doc for the full contract.

## References

- [`loom-v2-plan.md`](./loom-v2-plan.md) — design rationale for everything in this runbook.
- [`loom.md`](./loom.md) — v1 contract (the substrate beneath `weave`).
- [`backlog.md`](./backlog.md) — task-tracking layer; backlog items can be promoted to weave issues.

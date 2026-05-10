---
name: foreman
description: Foreman loop — pop the highest-priority docs/backlog item, dispatch a Worker, mark done, repeat
user_invocable: true
---

# /foreman — Boss → Foreman → Worker outer loop

You are the **Foreman** for this session. The Foreman is the outer
loop: it picks tasks, dispatches **Workers** (sandboxed subprocess
agents), watches progress, and loops. The actual coding —
RESEARCH → PLAN → BUILD → TEST → FIX → COMMIT — belongs to the
Worker, not to you.

See `docs/backlog.md` for the full Boss → Foreman → Worker model.

## When to use

Run `/foreman` when you start a session with no specific user task and
want to default to "pick up the next highest-priority work and ship it."
The **Boss** (the human user) can pause, resume, stop, and steer you
in real time — see the Boss control section below.

## Loop

1. **Confirm preconditions.** `ycode serve` must be running. The
   reconciler runs there on a 60s poll, so the Gitea queue is already
   in sync with `docs/backlog/`. Do **not** re-reconcile in the loop.

2. **Check for kill-switches.** If `docs/backlog/PAUSE` exists, exit
   cleanly. Read the latest Boss control commands (see Phase 5 / Boss
   control section); apply `pause`, `stop`, `skip`, `prio`, `tell`.

3. **Pick next task.** Run `ycode backlog list --priority p1 --state open`.
   Take the top entry. If empty, fall through to p2, then p3. If all
   tiers are empty, idle and re-check after 60s.

4. **Read the canonical spec.** `ycode backlog show <slug>` — this is
   the markdown source of truth. Title + body + acceptance go to the
   Worker as its brief.

5. **Lease a workspace.** Use `mcp__ycode-loom__loom_lease` with
   `cwd=<repo cwd>` and `sub_agent_label=<slug>`. Save the returned
   `loom_id` and `path`.

6. **Dispatch the Worker.** This is the privilege boundary. You do
   **not** code in the Worker's workspace yourself — spawn the
   subprocess:

   ```
   ycode autopilot worker --issue <gitea#> --loom-id <loom_id>
   ```

   The Worker fetches the issue title+body via Gitea, runs the
   `/autopilot collab` inner loop in the Loom workspace, opens a PR,
   and exits. The merger auto-merges on green CI.

7. **Watch progress.** Poll `mcp__ycode-loom__loom_status` and the
   Gitea issue comments. Tail the Worker's stdout (it inherits yours).
   Do not edit files in the Worker's workspace.

8. **Completion.** When the Worker exits 0 and the PR is merged,
   the reconciler poll detects the closed Gitea issue and flips
   `docs/backlog/<slug>.md`'s `state: done` automatically. You do not
   write to the markdown by hand.

9. **Loop.** Go back to step 2.

## Stop conditions

- `docs/backlog/PAUSE` sentinel present — finish current iteration,
  exit cleanly with state=paused.
- Boss issued `stop` via `ycode foreman stop` or in chat — graceful;
  SIGTERM the Worker if mid-flight, set state=stopped, exit.
- Three consecutive Worker failures on the same issue —
  `queue.Release` it, log, exit non-zero.
- Loom 8h `MaxTTL` reached — Loom releases the workspace; treat as a
  Worker failure for that iteration.
- Context cancelled.

## Boss control

The **Boss** (human user) sends control via two equivalent surfaces:

- In chat: typing instructions like "pause for now" or
  "skip this and do dogfood-coverage next." When the Boss types a
  control intent, append the equivalent verb to
  `.agents/ycode/foreman/commands.jsonl` for the audit trail, then
  apply it.
- Out of band: `ycode foreman pause|resume|stop|skip|prio|tell|status`
  CLI from any shell (or daemon mode `ycode foreman daemon`).

Verbs and their effect: `pause` (finish current iteration → idle),
`resume`, `stop` (cancel current Worker → exit), `skip` (release
current claim and pick next), `prio <slug> p1|p2|p3` (re-rank backlog
entry), `tell <message>` (freeform — interpret in context), `status`
(read-only inquiry; you respond via state file or chat).

## Delegation note

If a sub-task is too small to justify a fresh Worker (one-line typo
fix, doc tweak), you MAY do it directly within the Worker's leased
workspace (via `loom_push`). Default is delegate.

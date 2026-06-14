# Weave — Cleanup & State-Reconciliation UX Plan

Status: design plan. Companion to [`loom-v2-plan.md`](./loom-v2-plan.md) (substrate
design) and [`weave-runbook.md`](./weave-runbook.md) (operator walkthrough).

This document captures friction found while cleaning up a stale weave queue and
proposes a prioritized set of fixes. The work all orbits one root cause:

> **The weave queue doesn't know what git knows.** An item's recorded `State`
> drifts from the real merge status of its branch, and none of the lifecycle
> verbs (`list`, `pull`, `prune`) reconcile the two.

## Background — the session that surfaced this

Two `p0` items (issues 7 and 8) sat in `submitted` state long after their work
had been merged into `main` out-of-band. Verifying this and retiring them took a
manual git investigation that the tool should have done itself:

- `weave list` showed 2 items; `weave prune` found **10** terminal items with
  leftover sandbox directories silently consuming disk.
- `prune` refused the 2 `submitted` items (by design — see below), so the only
  escape was `abandon`, which *reads like discarding work* even though the work
  was already in `main`.
- Confirming "already merged" required: read `queue.json` → find sandbox paths →
  `git -C <sandbox> log` → `git merge-base --is-ancestor <sha> HEAD` per item.
- `prune` reported `skipped: branch not in user repo` for all 10, because agent
  branches never get fetched into the user repo — its merge-safety check was a
  no-op.

None of this is catastrophic, but every step is friction the substrate is
positioned to remove.

## Design principle

One addition to the v2 principles, specific to lifecycle management:

> **The queue is a cache of git reality, not a separate source of truth.**
> Any verb that reads or mutates lifecycle state first reconciles the recorded
> `State` against the branch's actual merge status. State that contradicts git
> is a bug to surface, not a value to trust.

---

## Findings & fixes

Prioritized. P0/P1 share one root cause and should land together.

### P0 — Reconcile `submitted` against merge status

**Problem.** `runWeavePrune` (`cmd/ycode/weave_impl.go:2711-2716`) treats only
`done | abandoned | failed | killed` as terminal — `submitted` is deliberately
excluded so it stays "awaiting `pull`". But the happy path (`pull` → `prune`)
breaks the instant work lands in `main` by any route other than `weave pull`
(direct merge, another agent, a sibling weave that absorbed it). The item is
then stranded: `prune` won't touch it, and `abandon` is the only exit — with
semantics that imply data loss.

**Fix.** Reconcile before classifying. When an item is `submitted` (or
`working` that crashed) and its branch tip is an ancestor of `main`, flip it to
a terminal state — either reuse `done` or introduce a distinct `merged` state —
and let `prune` sweep it. Run the reconciliation in `list`, `pull`, and `prune`
so the board stops lying regardless of entry point.

```
merged := gitMergeBaseIsAncestor(sandboxHeadSha, mainRef)   // see P1
if it.State == "submitted" && merged {
    it.State = "merged"   // now prunable; list shows it as resolved
}
```

**Acceptance.** A `submitted` item whose work is already in `main` is reported
as merged by `weave list` and removed by `weave prune` with no `abandon`.

### P1 — Verify merge status against the sandbox HEAD, not the user-repo branch

**Problem.** `prune`'s branch deletion uses `git branch -d` against the **user
repo** (`weave_impl.go:2756`) — only deletes if merged, which is correct intent.
But agent branches (`agent/weave-issue-N`) live only in the sandbox clones;
they're never fetched into the user repo. So the check always falls through to
`skipped: branch not in user repo` (`:2762`) and never actually evaluates merge
status. All 10 items in the session hit this path.

**Fix.** Evaluate merge status against the **sandbox HEAD sha**, which is
already recorded at terminal time (see the "HEAD sha at terminal time" comment,
`weave_impl.go:333`):

```
git merge-base --is-ancestor <sandbox-head-sha> <main-ref>
```

This is the primitive both P0 and `weave status` (P4) depend on. Factor it into
one helper.

**Acceptance.** `prune` correctly distinguishes merged from unmerged sandbox
work without requiring the branch to exist in the user repo.

### P2 — One consistent confirmation surface for destructive verbs

**Problem.** Two inconsistencies:

1. `prune` and `reset` accept `--yes`; `abandon` does **not** (discovered only
   by flag-error fallthrough during the session).
2. `prune`'s prompt reads `os.Stdin` blindly via `Fscanln`
   (`weave_impl.go:2734`), whereas `reset` gates on `stdinIsTTY`
   (`:2484`). In a non-TTY / agent context, `prune` prints an unanswerable
   prompt, cancels, **and dumps full usage twice** with exit 2.

**Fix.** A single shared `confirmDestructive(cmd, mode, msg, yes)` helper:

- TTY + no `--yes` → prompt.
- non-TTY + no `--yes` → single clean error ("refusing destructive op without
  `--yes` in non-interactive mode"), no usage dump.
- `--yes` or JSON mode → proceed.

Wire it into `abandon`, `kill`, `reset`, and `prune`, and give `abandon` a
`--yes` flag for symmetry.

**Acceptance.** Every destructive verb behaves identically under TTY / non-TTY /
`--yes`, with no usage-dump noise on the refusal path.

### P3 — Surface terminal clutter in default `weave list`

**Problem.** Default `list` shows only non-terminal items (`activeOnly`,
`weave_impl.go:673`). Terminal items with leftover sandbox directories are
invisible until something tries to clean them — disk grows silently.

**Fix.** Add a one-line footer to `list` (same pattern as the existing
cross-repo footer):

```
+10 terminal items, ~N MB reclaimable — run 'weave prune'
```

Count comes from the same classification used by `prune`; size from `du` of the
sandbox dirs (best-effort, omit on error).

**Acceptance.** `weave list` makes reclaimable clutter visible without
`--all`/`--history`.

### P4 — `weave status <N>` — answer "is this merged?" directly

**Problem.** The most common operator question about a `submitted` item — "is
this already in `main`?" — required manual git archaeology across `queue.json`
and the sandbox clone.

**Fix.** Add `weave status <N>` reporting:

- state (reconciled per P0/P1),
- branch + sandbox HEAD sha,
- **merged-into-main: yes/no** (the P1 primitive),
- commits ahead of base,
- last recorded verify result.

Most of this is already captured at terminal time (`weave_impl.go:112`, `:333`).

**Acceptance.** `weave status 7` answers the merge question in one command, no
git knowledge required.

### P5 — Minor: log summary and title rendering

**Problem.** `weave log` dumps the raw PTY capture with no completion signal —
`tail` lands mid-diff. Issue 8's title rendered `recurrence �...` in `list`
(emoji truncated mid-rune).

**Fix.**

- `weave log --summary` → last verify line + terminal state only.
- Truncate titles on rune boundaries (`[]rune`), not bytes.

**Acceptance.** `log --summary` gives a one-glance outcome; no mojibake in
`list`.

---

## Sequencing

1. **P1** first — the `mergeBaseIsAncestor(sandboxHead, main)` helper is the
   primitive P0 and P4 both consume.
2. **P0** on top of it — state reconciliation in `list` / `pull` / `prune`.
   Together P0+P1 dissolve the core friction.
3. **P2** — independent, small, ships anytime.
4. **P3 / P4 / P5** — cheap observability adds; order by appetite.

## Touch points

All in `cmd/ycode/weave_impl.go` (+ flag wiring in `cmd/ycode/weave*.go` and
output helpers in `internal/cli/weavecli/`):

- `runWeavePrune` (`:2685`) — classification, reconciliation, sandbox-head check.
- list builders (`:673`, `:759`, `:2338`) — reconciliation + footer.
- `pull` path — reconciliation on absorb.
- new `confirmDestructive` helper — shared by `abandon` / `kill` / `reset` /
  `prune`.
- new `runWeaveStatus`.

## Out of scope

- Changing the `submitted` semantic itself (it remains "work finished, awaiting
  absorb" — we only stop it from going *stale* when git moves underneath it).
- Auto-pruning without an explicit verb. Reconciliation changes what `prune`
  *can* sweep; it never deletes on its own.

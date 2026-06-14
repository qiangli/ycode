# Weave — Dogfooding Retro & Enhancement Roadmap

Status: **proposed** (gaps catalogued from a live multi-round dogfooding
session; nothing here built yet). Companion to
[`loom-v2-plan.md`](./loom-v2-plan.md) (substrate design),
[`loom-v2-implementation.md`](./loom-v2-implementation.md) (build notes),
[`weave-runbook.md`](./weave-runbook.md) (operator walkthrough), and
[`weave-cleanup-ux-plan.md`](./weave-cleanup-ux-plan.md) (queue↔git
reconciliation, already implemented).

> **North star.** `ycode weave` should be the best-of-its-kind substrate for
> fanning *heterogeneous, foreign* agentic CLIs (codex, claude, gemini, aider,
> opencode, …) across an isolated-clone queue and converging only verified
> work. The cleanup plan fixed *queue↔git* drift. This plan fixes the next
> layer: **the substrate's blind spots when the worker is a foreign TUI it does
> not control** — provisioning, prompt/approval delivery, liveness, and
> verified convergence.

---

## The session that surfaced this

Two weave rounds against the `sh` submodule (bashy = Bash 5.3 drop-in), goal:
reduce the bash 5.3 fixture diffs. Six issues across codex / claude / gemini
(aider + opencode could not be used — see Gap B1). Outcome:

- **Round 1 was invalid** — every sandbox was missing the test corpus, so no
  worker could measure and one gate *false-passed at diff = 0*. Salvaged one
  real commit by hand.
- **Round 2 (after the fixes below were applied manually) worked**: 3 of 4
  workers produced verified wins (assoc 243→216 + flap fixed; new-exp 225→163;
  nameref 631→576); 1 worker (array) produced a net-negative patch that the
  *full-suite* judge caught and rejected (its single-fixture gate would have
  passed it).

Every fix I applied by hand in round 2 — symlinking the corpus into each
sandbox, clearing trust prompts, answering approval prompts, polling for
stalls, judging on a scratch tree with the *whole* suite — is something the
substrate is positioned to do itself. That is the backlog below.

### What already works well (keep)

- Isolated full-clone sandboxes per issue; `--no-spawn` prep hook; `--resume`.
- Queue-as-facts JSON (`list --json`) with `state`/`exit`/`killed_by`.
- `say` for live steering; `kill` is non-destructive (commits preserved).
- Watchdogs: `--max-runtime`, `--mem-limit`, `--idle-timeout`.
- Queue↔git reconciliation (cleanup plan P0–P6).
- Double-up safety: N instances of one tool, each its own sandbox, no conflict.

---

## Gap catalog

Prioritized P0 (blocks unattended foreign-CLI rounds) → P3 (polish). Each gap:
**symptom (evidence) → root cause → proposed enhancement → effort**.

### A. Sandbox provisioning

#### A1 (P0) — Untracked / gitignored corpora are absent from sandboxes
- **Symptom.** Round 1: every sandbox lacked `external/bash-5.3/tests`; workers
  could not run the repro; gates `cd`'d into a missing dir and silently
  measured nothing. `external/bash-5.3` is a **gitignored symlink** to an
  absolute path outside the repo, so `git clone` reproduces neither the symlink
  nor its target.
- **Root cause.** A sandbox is a clean clone = tracked files only. "Seed" in the
  substrate today means *filing issues* (`add --from-file`), not *seeding a
  workspace*. There is no declaration of "this issue needs these untracked
  paths."
- **Proposed.** A per-issue / per-queue **workspace seed manifest**:
  `weave add --seed '<src>:<dst>'` (repeatable) or a `.weave/seed.toml` at repo
  root listing paths to symlink/copy into every sandbox after clone, before
  launch. Bonus: **auto-detect** gitignored symlinks in the source tree and
  offer to recreate them (`weave doctor` warns; `start` recreates). Seeds are
  never committed (already gitignored).
- **Effort.** S–M. Highest leverage — without it, fixture-bearing repos can't be
  weaved at all.

#### A2 (P2) — No pre-launch workspace validation
- **Symptom.** The missing corpus was only discovered *after* launching 4
  workers that then did nothing.
- **Proposed.** `weave doctor <N>` / `start --check`: run the issue's REPRO/gate
  preamble in the prepped sandbox and assert it produces a sane baseline (e.g.
  the gate's measured number is finite and matches the issue's stated baseline)
  **before** spending a worker on it. Catches missing seeds, broken gates, and
  wrong baselines up front.
- **Effort.** M. Pairs with D1.

### B. Prompt & approval delivery to foreign CLIs

#### B1 (P0) — The task prompt is delivered as an argv positional; some tools treat it as a filename
- **Symptom.** `start -- aider "<body>"` → `OSError: File name too long`;
  `-- opencode "<body>"` → `ENAMETOOLONG`. Both treat a positional as a file to
  open. codex/claude/gemini happen to treat it as a prompt, so they worked.
- **Root cause.** One argv convention for all tools; no per-tool input-channel
  adapter.
- **Proposed.** A **per-tool launch adapter** registry mapping each known tool
  to its correct prompt channel: argv-prompt, `--message`/`--message-file`,
  stdin, or "launch bare then inject via the PTY." Weave already stores the
  body — write it to a sandbox file and hand the tool its own message-file flag,
  or type it into the live session, per the adapter. Foreign tools should be
  first-class, not "happens to work."
- **Effort.** M. Unlocks the full fleet (aider/opencode today; future tools
  cheaply).

#### B2 (P0) — Startup trust prompts and mid-run approval prompts park workers silently
- **Symptom.** Every tool showed a folder-trust prompt at launch; gemini's
  needed re-answering and it sat parked (0 edits) until cleared. Mid-run, codex
  parked on an "Allow this command? 1.Yes/2.No" approval before it could commit.
  A headless worker that parks reads as `state=working` while doing nothing and
  burns its `--max-runtime` to a kill.
- **Root cause.** Weave knows neither each tool's *skip-approvals* flag nor its
  *trust-cache* location, and does not watch for or answer prompts.
- **Proposed.** Extend the per-tool registry (B1) with:
  1. the tool's **skip-approvals/yolo flag** (codex
     `--dangerously-bypass-approvals-and-sandbox`, claude
     `--dangerously-skip-permissions`, gemini `--yolo`, aider `--yes-always`),
     applied by default for sandbox launches (safe: isolated, no-push clone) —
     opt-out, not opt-in;
  2. the tool's **trust-cache key**, pre-seeded during `--no-spawn` prep so the
     dialog never appears (defense level 2);
  3. a built-in **prompt watcher** (defense level 3) that matches generic prompt
     signatures (`trust`, `allow`, `approve`, `Yes/No`, `press enter`) on the
     PTY and auto-answers the affirmative, logging each answer.
- **Effort.** M. This + B1 is what makes a round actually run unattended.

### C. Liveness & stall detection for non-MCP workers

#### C1 (P1) — `state=working` is not liveness; foreign CLIs have no MCP heartbeat
- **Symptom.** Workers sat at `working` while parked on a prompt, idle after a
  completion summary, or looping (codex re-claimed "passing" for 20 min without
  amending). I detected all three only by manually diffing log tails + checking
  `commits_ahead` deltas on a cadence.
- **Root cause.** The substrate's liveness model is **MCP-session keepalive = the
  heartbeat** (`loom-v2-plan.md` §Liveness; `loom-v2-implementation.md` N0.5
  flags "a fallback heartbeat verb may be unavoidable"). Foreign CLIs driven
  over a PTY maintain **no** MCP session to weave, so the only signal is
  `--idle-timeout` (last PTY byte), which is blunt: a looping or self-narrating
  tool keeps emitting bytes while making zero progress.
- **Proposed.** A **progress heartbeat from observable git/PTY signals**, not
  session keepalive: per-lease track `last_output_at`, `commits_ahead` and
  `files_changed` deltas between samples, and a cheap loop-signature heuristic
  (same command/file repeated N×). Surface a derived `health`:
  `progressing | parked | looping | idle-done`. Expose via `list --json` and a
  `weave watch` dashboard (the runbook's heartbeat column, computed for
  non-MCP tools). Optionally auto-`say` a nudge or escalate to the orchestrator
  on `parked`/`looping`.
- **Effort.** M. Turns the manual liveness-polling discipline (now codified in
  the orchestrator instruction) into a substrate signal.

#### C2 (P2) — `commits_ahead` is unreliable in the queue
- **Symptom.** `list --json` showed `commits_ahead: null` for active workers; I
  had to `git -C <sandbox> rev-list --count <base>..HEAD` myself, and `@{u}..`
  returned 0 because agent branches have no upstream.
- **Proposed.** Always compute `commits_ahead` against the lease's recorded base
  commit (not `@{u}`), refreshed on each `list`. It is the cheapest real
  progress signal and feeds C1.
- **Effort.** S.

### D. Verify gates — guard against false pass and silent regression

#### D1 (P0) — Gates can false-pass on a broken precondition
- **Symptom.** Round-1 gate: `cd external/bash-5.3/tests` failed → `diff` vs a
  missing `.right` produced 0 lines → `[ 0 -lt 243 ]` **passed**. A green gate
  attested a tree that never measured anything.
- **Root cause.** Gates are opaque shell; a failed precondition (missing dir,
  missing file, build error) is indistinguishable from a real pass.
- **Proposed.** Gate **preconditions/guards** as a first-class concept: a gate
  declares required paths/commands; the runner asserts them and marks the gate
  `errored` (not `passed`) if any fail. At minimum ship a **gate template** with
  the guards baked in (`test -f <fixture> || { echo MISSING; exit 1 }`, build
  check, then the measurement) and document the "echo every measured number,
  end with the explicit `-lt` test" contract the cleanup plan already lives by.
- **Effort.** S–M.

#### D2 (P1) — Single-target gates miss sibling regressions
- **Symptom.** The array worker's gate measured only array (184→170 = pass) but
  its change regressed two *other* fixtures from PASS→FAIL. Only judging on the
  **full suite** caught it.
- **Root cause.** A gate proves the target improved; it cannot prove the rest of
  the world didn't break.
- **Proposed.** A **regression guard**: let an issue reference a baseline
  snapshot (e.g. a `weave baseline` capture of the full test scoreboard) and have
  `pull`/judge re-run it on the *merged* tree, refusing any item that regresses a
  previously-green signal. This is the "re-measure on the merged checkout" step
  from the orchestrator instruction, automated.
- **Effort.** M.

### E. Judge & merge ergonomics

#### E1 (P1) — No one-command "apply this lease's commits onto current main and run X"
- **Symptom.** To judge each lease honestly (full suite, on the post-merge tree,
  with the corpus present) I manually: `git diff base..head > patch`, `git apply`
  to my checkout, build, run the scoreboard ×3, then `git checkout --` to revert.
  Repeated per lease, per re-steer.
- **Proposed.** `weave try <N> [-- <cmd>]` (a.k.a. `judge`): create a throwaway
  worktree off current `main`, apply lease N's commits (auto-rebase onto the new
  base — the cleanup plan notes resumed leases land on a stale base), run the
  given command (default: the issue's gate or a configured suite), report
  pass/fail + measured output, and discard the worktree. `pull --dry-run` is the
  lighter cousin. This makes "the merged artifact equals the attested tree" a
  one-liner instead of a manual ritual.
- **Effort.** M. Biggest day-to-day ergonomics win for the orchestrator role.

### F. Watchdog grace

#### F1 (P2) — Hard `--max-runtime` kill discards uncommitted rework
- **Symptom.** codex was steered to fix a regression, worked ~10 min more, then
  hit its 30 m `--max-runtime` and was SIGKILLed **mid-amend** — the rework was
  uncommitted and lost; HEAD was still the bad commit.
- **Root cause.** `--max-runtime` is a hard wall; the reaper "grace" in the docs
  is about *sandbox retention after idle*, not "let the worker save first."
- **Proposed.** A **grace-commit window**: on max-runtime expiry, send the worker
  a soft signal + a `say "commit what works now and exit"`, wait a short bounded
  window, then hard-kill. At minimum, snapshot the dirty tree to a
  `weave/autosave` ref so re-steer/salvage can recover it. (Pairs with the
  issue-body "commit what works + BLOCKERS.md" contract.)
- **Effort.** M.

### G. Tool capability registry

#### G1 (P3) — Per-tool strengths/quirks live only in the orchestrator's head
- **Symptom.** Which tool accepts argv prompts, which crashes, which over-reaches
  (array), which is reliable for surgical work — all tracked manually this
  session and codified prose-only in the orchestrator instruction.
- **Proposed.** A persisted, machine-readable **tool registry** (the B1/B2
  adapter data + observed scorecard: success rate, regression rate, mean
  convergence time per points). `weave start` can warn on a known-bad
  pairing and `prio`/assignment can consult it.
- **Effort.** M. Builds naturally on B1/B2.

---

## Roadmap & sequencing

The P0 set is the **"unattended foreign-CLI round"** milestone — with it, a
human (or an orchestrator agent) files bite-size issues and the substrate
provisions, launches, keeps alive, and refuses false/regressing work without
hand-holding.

1. **B1 + B2 (per-tool launch adapter: prompt channel + skip-approvals +
   trust-seed + prompt watcher).** One registry, the highest unlock — turns
   "happens to work for 3 tools" into "works for the fleet, unattended."
2. **A1 (workspace seed manifest).** Without it, fixture/corpus repos are
   un-weavable. Small and independent.
3. **D1 (gate guards / template).** Stop false passes. Small, ships anytime.
4. **C2 (reliable `commits_ahead`)** → **C1 (progress heartbeat + health +
   `weave watch`).** Liveness for non-MCP tools.
5. **E1 (`weave try`/judge worktree)** + **D2 (regression guard).** Verified
   convergence on the merged tree, one command.
6. **A2 (pre-launch validation), F1 (grace-commit), G1 (tool registry).**
   Robustness + polish.

## Competitive framing — "best of its kind"

Most multi-agent harnesses assume **one** vendor's agent and a cooperative API.
Weave's differentiator is **vendor-neutral orchestration of foreign CLIs over
real isolated clones with git as the source of truth.** The gaps above are
exactly the seams that show when the worker is a TUI you don't own:

- *Provisioning* (A) — real repos have untracked corpora; a clone isn't enough.
- *Delivery* (B) — every CLI has its own prompt/approval/trust idiosyncrasies.
- *Liveness* (C) — no MCP session means infer progress from git + PTY.
- *Verification* (D/E) — never trust a worker's self-report; re-derive on the
  merged tree, full-suite, with guards against false-pass and regression.

Closing them makes weave the substrate that runs *any* coding agent unattended
and merges *only* what reproduces — which nothing else does well today.

## Out of scope (for now)

- Replacing the MCP-session heartbeat for *integrated* (ycode-native) tools —
  C1 is the **fallback** path for foreign tools; the MCP path stays for native.
- Distributed/multi-host scheduling (a separate concern from worker fidelity).
- Auto-decomposition of goals into issues (sprint planning stays human/
  orchestrator-driven; see the orchestrator instruction).

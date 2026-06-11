---
name: weave
description: Orchestrate parallel subagent CLIs (codex, claude, opencode, ...) against a queue of issues — isolated sandboxes, live steering, recovery, verified convergence
user_invocable: true
---

# /weave — Multi-Agent Orchestration Playbook

Drive `ycode weave` as an orchestrator: file issues, fan out one
subagent CLI per issue in isolated git-clone sandboxes, watch and
steer them, recover from kills, and merge verified work back. Every
rule below was earned in dogfooding; the failure it prevents is
named where that helps.

Use this when you have N independent pieces of work for peer agent
CLIs. For one inline edit in the current checkout, just do the work.

## Phase 0 — Before filing anything

- `cd` to the target repo root. Queues are per-repo, keyed by cwd;
  an empty `weave list` elsewhere now hints where the action is.
- Identify anything the task's build/repro needs that is NOT in git
  (vendored corpora, symlinked fixtures, generated assets). Sandbox
  clones contain ONLY tracked files — a missing corpus is why one
  dogfood agent escaped its sandbox hunting for fixtures and another
  couldn't verify at all.

## Phase 1 — File issues (the issue-body contract)

One issue per independent task. Every body carries, in order:

1. **Sandbox contract** (verbatim-ish): "Your cwd is your isolated
   sandbox — a full git clone. Stay inside it; use only relative
   paths; never follow git remotes. Your branch is checked out and
   your git identity is pre-set."
2. **GOAL** — one measurable outcome.
3. **REPRO** — the exact command that measures it, runnable from the
   sandbox. If the canonical harness applies filters/env, embed them.
   Anchor binaries BEFORE entering linked corpora: `BIN=$PWD/bin/tool`
   at the repo root, never `realpath ../..` from inside a symlinked
   directory — it resolves to the symlink target's checkout and the
   agent silently measures the wrong build.
4. **SCOPE** — directory allowlist, disjoint from every other issue
   running in parallel (parallel agents must not be able to collide
   even if both succeed).
5. **SUCCESS CRITERIA** — build green, tests green, and: "state the
   EXACT measured number from the repro in your commit message; an
   unverified claim is worse than none." (An agent once claimed a
   25% improvement that did not reproduce.)
6. **Commit discipline** — `git add` files BY NAME; commit to the
   current branch; DO NOT push; DO NOT switch branches.
7. **Blockers escape** — "if stuck after 3 serious attempts, commit
   what works plus <TOPIC>-BLOCKERS.md with your diagnosis — include
   a verified patch if the fix lies outside your scope — then exit
   0." (A scoped agent once delivered the complete out-of-scope fix
   this way; the orchestrator applied it. Best outcome available.)
8. **Exit instruction** matched to the tool (see Phase 3): headless
   tools "exit cleanly (exit 0)"; steerable TUI runs "reply DONE and
   wait" (the orchestrator stops them).

## Phase 2 — Allocate and prepare sandboxes

When the repro needs untracked content, allocate first, link, then
launch into the prepared sandbox:

    ycode weave start --no-spawn --issue N
    ln -s /abs/path/to/corpus  <sandbox>/corpus     # whatever Phase 0 found
    ycode weave start --resume --issue N ... -- <tool> "<body>"

Skip this phase when the repo is self-contained.

## Phase 3 — Launch (tool recipes + watchdogs)

ALWAYS set watchdogs on unattended runs — they are the reason a
runaway costs a retry instead of the machine:

    --max-runtime 45m --mem-limit 6g     # idle-timeout optional; TUI spinners defeat it

Per tool:

- **codex, headless**: `codex exec --full-auto "<body>"` — exits
  cleanly on completion; the easy default.
- **codex, steerable TUI**: `codex -s workspace-write -a never
  "<body>"` — answers nothing by itself: expect the directory-trust
  prompt and clear it with `weave say N "1"`. Does not exit when
  done (see Phase 7).
- **claude**: NEVER bare `claude -p` for a run you want to watch or
  steer — it buffers ALL output until exit (empty capture, ignores
  injected keystrokes). Use `claude --dangerously-skip-permissions
  --verbose --output-format stream-json -p "<body>"` for a streaming
  headless run, or accept fire-and-forget and read its transcript
  under `~/.claude/projects/<sandbox-slug>/` for progress.
- **opencode**: `opencode run "<body>"` — streams live, exits clean.
  CONTAINMENT WARNING: opencode keeps persistent per-project state;
  if it has ever worked in the origin repo it gravitates back to it
  by absolute path — even with the sandbox's `origin` remote removed
  (observed twice: committed to the real checkout's master, sandbox
  branch left empty). Prefer codex/claude when sandbox containment
  matters; if you do use opencode, check `git -C <sandbox> log` AND
  the origin repo's HEAD at completion before trusting state.

Background each start (`&`); the wrapper auto-setsids.

## Phase 4 — Monitor

    ycode weave list             # STARTED + DUR columns show run age
    ycode weave log N -f         # live PTY capture (any number of watchers)
    ycode weave list --watch --json   # NDJSON state transitions

Never measure benchmarks/suites on the host while subagents compile
in parallel — per-test timeouts flake under load and read as
regressions.

## Phase 5 — Steer (weave say)

    ycode weave say N "btw, status check: one line — current measured
    number and what you're working on — then continue."

- One say = one typed line + Enter. Injected text is keystrokes; the
  agent treats it as user input.
- codex mid-turn QUEUES typed input on Tab instead of submitting:
  follow the say with a literal Tab via the control socket
  (`printf '\t\n' | nc -U <ctl_sock>`). Avoid a leading `/` on plain
  messages — codex parses it as a command-palette trigger.
- One stdin writer at a time. Two writers interleave in the composer
  (it happened); coordinate before injecting.

## Phase 6 — Recover

- Wrapper died / machine restarted: `weave list` flags stale items →
  `weave start --resume --issue N -- <tool> "<follow-up body>"`.
  Resume works from any state with a preserved sandbox.
- Watchdog killed mid-work: the sandbox survives. Inspect
  uncommitted changes — they are often real progress (one killed run
  held a 3x diff reduction uncommitted). Verify, commit them as the
  orchestrator, then resume with a short follow-up prompt listing
  only what remains.
- Agent finished but claimed results you can't see: NEVER trust the
  claim — re-run the issue's REPRO yourself before merging. Park
  unverifiable work on a branch; re-file with a hardened prompt.

## Phase 7 — Converge and verify

    ycode weave wait --all --timeout 50m
    ycode weave pull                      # merges working/submitted branches

- Steerable TUI runs don't exit on DONE. Stopping them with
  `weave kill N` preserves the branch but records state=failed —
  `pull` then SKIPS it. Merge manually:
      git fetch --no-tags <sandbox> agent/weave-issue-N:agent/weave-issue-N
      git merge --no-ff agent/weave-issue-N
  (`failed` after a kill is bookkeeping, not a verdict on the work.)
- After pull: rebuild, run the canonical measurement, then run the
  FULL suite on a QUIET machine — not just the fixtures the agents
  worked on. Cross-fixture ripple is real: one round improved its
  target fixture while silently flipping an unrelated one from PASS
  to FAIL by deleting a quirk-encoding block it didn't need to
  touch. Bisect any regression against the pre-merge commit before
  accepting the round.
- Escaped commit (work landed on the ORIGIN repo's branch instead
  of the sandbox): don't reflex-revert. Verify it canonically in
  place — exact repro, full package tests, inspect any test
  deletions for legitimacy. Keep it if real (record via
  `weave abandon N --reason`), reset and park on a branch if not.
- Clean residue: `weave abandon N --reason "<why, where the work
  went>"` — the reason is the audit trail.

## Failure modes, condensed

| Symptom | Likely cause | Move |
|---|---|---|
| `weave list` empty | wrong cwd | follow the hint; cd to the repo |
| capture empty, agent "working" | tool buffers (claude -p) | read its transcript; next time use streaming mode |
| agent cites paths outside sandbox | corpus missing in clone, or remote followed | Phase 2 links; origin is removed from sandboxes — verify `git remote` is empty |
| claimed improvement won't reproduce | agent measured differently | re-run the issue's exact REPRO; park the branch |
| state=failed after a kill you issued | kill bookkeeping | branch is intact; manual fetch+merge |
| exit 143, no commits | watchdog kill | check sandbox for uncommitted progress before re-filing |

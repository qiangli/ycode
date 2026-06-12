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

## Authoring --verify commands

The wrapper runs them with a hermetic `bash --noprofile --norc -c`
in the sandbox (10m ceiling). Still: never `bash -l` inside (user
dotfiles), never `set -e` around a `diff` pipeline (exit 1 means
"files differ", not failure), always `echo` the measured number
(it lands in verify_output — the evidence trail), and end with the
explicit gate test (`[ "$n" -lt <baseline> ]`). The gate refusing a
merge is `weave pull` reporting verify-failed; the orchestrator
re-runs the gate by hand before overriding anything.

## Quick start — any orchestrator, zero to fleet

You may be Claude Code, codex, gemini, opencode, or anything that
can run a CLI: the flow is identical, and you may enlist YOUR OWN
CLI as a worker too (each worker runs in an isolated clone, so
self-orchestration is safe). From a user goal:

    cd <repo-root>                       # queues are per-repo (cwd-keyed)
    # 1. DECOMPOSE the goal into N independent issues with DISJOINT
    #    file scopes; write each body per the Phase 1 contract below.
    #    Complex round? Run the optional Phase 1.5 planning poker.
    ycode weave add "<title>" --priority p0 --body "<body>"   # × N
    # 2. PREPARE (only if the build needs untracked corpora — Phase 2):
    ycode weave start --no-spawn --issue <N> && ln -s <corpus> <sandbox>/
    # 3. LAUNCH one worker per issue, backgrounded, watchdogs ON
    #    (tool recipes in Phase 3 — pick per tool, including your own):
    ycode weave start --resume --issue <N> --max-runtime 45m --mem-limit 5g \
        -- <tool> <tool-flags> "<body>" &
    # 4. MONITOR: `weave list` (TOOL/STARTED/DUR), `weave log <N> -f`,
    #    blocked-agent protocol (Phase 4); steer with `weave say`.
    ycode weave wait --all --timeout 50m
    # 5. CONVERGE: verify each claim, then merge + re-measure (Phase 7):
    ycode weave pull
    # 6. JUDGE: file one more issue on the merged state for an
    #    independent verification agent (end of Phase 7).

Tool cheat-sheet (details + caveats in Phase 3):

    claude    claude --dangerously-skip-permissions "<body>"        # TUI; pre-seed trust in ~/.claude.json (see Per tool) or say N "1"
    codex     codex exec --full-auto "<body>"                       # headless, exits clean
    gemini    gemini --yolo --skip-trust -i "<body>"                # TUI; no trust dialog
    opencode  opencode run "<body>"                                 # headless; check artifacts, not exit code
    aider     aider --yes-always --no-check-update --message "<body>"  # headless; auto-commits; model from ~/.aider.conf.yml

## Tool report card (update as evidence accumulates)

Reflecting seven dogfood rounds:

- **codex**: reliable workhorse; honest no-ops (declines to commit when its
  change regresses the metric); headless exec exits clean.
- **claude**: strongest on deep multi-file work (delivered both fixture flips);
  TUI needs trust-dialog answer + graceful /exit stop.
- **opencode**: best as verification judge; ingests `say` steering while
  headless; check artifacts not exit codes (permission rejections can end runs
  with exit 0).
- **aider**: passed probation on a gated 3-pointer (sh new-exp
  anchored-substitution fix, −21 diff lines, zero collateral, ~$0.02).
  Reliable surgical edits when the issue body pins exact cases and
  files; auto-commit removes the forgot-to-commit failure mode. Two
  caveats: it resolves test-name collisions by DELETING its own new
  tests (tell it to rename instead — a follow-up resume fixed it on
  the first ask), and it cannot iterate against a test suite in
  --message mode, so the verify gate is the only backstop. Give it
  well-specified one-shot edits, not exploratory work.
- **gemini**: currently weakest — stalled 30 min on a usage-limit menu,
  unresponsive to /exit and /quit, one rejected branch that failed verification
  (claimed improvement, measured regression); also writes a GEMINI.md context
  file into any cwd it is invoked in, even for --help. Prefer headless -p for
  low-stakes tasks; keep p0 work elsewhere until it earns up.

Everything below is the depth behind those six steps — read Phase 1
(issue contract) and Phase 7 (verification) in full before your
first round; skim the rest and return on demand.

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

## Phase 1.5 — Sprint planning (OPTIONAL — the orchestrator's call)

For complex multi-issue rounds, get second opinions on priority and
effort BEFORE assignment. Skip it for small or obvious rounds; when
used, timebox the whole phase to a few minutes.

1. Compile the issue list (titles + 2-line summaries + proposed
   scopes) into one planning brief with this rubric:
   "Story-point each issue on the Fibonacci scale 1,2,3,5,8 where
   8 ≈ 30 minutes of agent runtime — the hard cap. If you judge an
   issue larger than 8, do not stretch the scale: propose the split
   into smaller issues. Also rank priority (p0-p3) by value and
   unblocking effect. Return STRICT JSON:
   [{id, points, priority, split: [titles...]|null, note}]."
2. Fan the brief out as CHEAP HEADLESS one-shots — these are
   advisory calls, not sandboxed issues (no repo access needed):
       codex exec "<brief>"          claude -p "<brief>"
       gemini -p "<brief>"           opencode run "<brief>"
   All four in parallel; collect within minutes.
3. Reconcile — the orchestrator has FINAL SAY: take the median
   points per issue; any tool-to-tool spread wider than one Fib
   step, or any split proposal, means the issue is under-specified
   — break it down and re-add the pieces. Record the outcome:
       ycode weave point <issue> <1|2|3|5|8>
       ycode weave prio  <issue> <p0..p3>
   (or file with `weave add --points N` directly). PTS shows in
   `weave list`.
4. Assign per the report card and the points: budget --max-runtime
   from points (8 → 30m + small margin), biggest issues to the
   strongest tool first.

## Phase 2 — Allocate and prepare sandboxes

When the repro needs untracked content, allocate first, link, then
launch into the prepared sandbox:

    ycode weave start --no-spawn --issue N
    ln -s /abs/path/to/corpus  <sandbox>/corpus     # whatever Phase 0 found
    ycode weave start --resume --issue N ... -- <tool> "<body>"

Skip this phase when the repo is self-contained.

## Phase 3 — Launch (tool recipes + watchdogs)

Calibrate `--mem-limit` to the TARGET REPO's build profile, not a
global default: a Go repo that links a 500MB binary peaks ~5GB RSS
in the linker alone — a 5g cap killed three builders in one round
(the watchdog forensics named `link` each time). Budget link-peak
plus headroom (12g served).

When verifying agent work via e2e suites that drive a compiled
binary, REBUILD that binary first (make compile / the repo's
equivalent) — judging fresh code against a stale harness binary
produces phantom failures (nearly cost one agent a correct fix).

ALWAYS set watchdogs on unattended runs — they are the reason a
runaway costs a retry instead of the machine:

    --max-runtime 45m --mem-limit 6g     # idle-timeout optional; TUI spinners defeat it

Two watchdog facts: timers count AWAKE time (macOS sleep pauses Go
timers, so DUR can show hours against a 30m cap — not a bug), and
TUI agents don't self-pace toward the deadline the way headless
exec modes do. Give interactive runs a soft budget inside the
prompt ("commit whatever passes by minute 35") or expect to recover
orphaned work after the kill.

## Startup-prompt defense in depth (every tool, every launch)

Interactive tools block on trust/permission prompts; headless ones may
silently no-op instead (worse — exit 0, no artifacts). Apply THREE
levels, in order, for every launch:

1. **CLI flag** — use it when it exists.
2. **Pre-seed config** — when no flag covers it, write the tool's own
   trust/permission cache for the sandbox path during prep (the exact
   key the tool's dialog would write).
3. **Monitor + `weave say`** — ALWAYS, even with 1+2: for the first
   ~90s of any TUI launch, poll `weave log N -n 6` for prompt
   signatures — match generic words (`trust`, `approve`, `allow`,
   `Yes/No`, `press Enter`), NOT exact menu text (wording shifts
   between tool versions) — and answer with `weave say N "<answer>"`.

Per-tool matrix (this machine, update as versions change):

| tool | 1) flags | 2) pre-seed | 3) watch for |
|---|---|---|---|
| claude TUI | none for trust (--dangerously-skip-permissions ≠ trust) | `~/.claude.json` → `projects.<sandbox>.hasTrustDialogAccepted=true` (+ hasCompletedProjectOnboarding) | "trust" dialog → say "1" |
| codex exec | `--skip-git-repo-check --sandbox workspace-write` (no prompts in exec mode) | `~/.codex/config.toml` → `[projects."<sandbox>"]\ntrust_level = "trusted"` (needed for TUI mode) | TUI: directory-trust → say "1" |
| gemini | `--yolo --skip-trust` (covers everything) | n/a | usage-limit menus (it stalls there) |
| opencode | NO flags; headless `run` silently REJECTS un-permitted tools and exits 0 — the no-op-with-green-exit failure mode | write `opencode.json` into the sandbox root at prep: `{"permission":{"edit":"allow","bash":"allow","webfetch":"allow"}}` (never commit it) | artifacts, not prompts: zero commits + clean exit = rejection happened |
| aider | `--yes-always` (covers all confirmations) | n/a (model/keys via ~/.aider.conf.yml + ~/.env) | reasoning loops ("Already done…" repetition) — kill, don't wait |

Pre-seed snippets (run during Phase 2 prep, $SANDBOX = absolute path):

    # claude
    python3 - "$SANDBOX" << 'EOF'
    import json,sys,os
    p=os.path.expanduser('~/.claude.json'); d=json.load(open(p))
    d.setdefault('projects',{}).setdefault(sys.argv[1],{}).update(
        hasTrustDialogAccepted=True, hasCompletedProjectOnboarding=True)
    json.dump(d,open(p,'w'),indent=2)
    EOF
    # codex (TUI mode only; exec mode needs no seed)
    printf '\n[projects."%s"]\ntrust_level = "trusted"\n' "$SANDBOX" >> ~/.codex/config.toml
    # opencode
    printf '{"permission":{"edit":"allow","bash":"allow","webfetch":"allow"}}' > "$SANDBOX/opencode.json"

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
- **claude TUI trust dialog**: there is NO flag to pre-trust a folder
  (`--dangerously-skip-permissions` does not cover it; only `-p` mode
  skips trust). PRE-SEED the per-directory cache during sandbox prep
  instead — upsert into `~/.claude.json`:
      python3 - "$SANDBOX" << 'EOF'
      import json,sys,os
      p=os.path.expanduser('~/.claude.json'); d=json.load(open(p))
      d.setdefault('projects',{}).setdefault(sys.argv[1],{}).update(
          hasTrustDialogAccepted=True, hasCompletedProjectOnboarding=True)
      json.dump(d,open(p,'w'),indent=2)
      EOF
  Undocumented but it is exactly the key the dialog itself writes.
  Fallback if the dialog still appears: `weave say N "1"` — and match
  on the word "trust" in the log, not exact menu text (the dialog
  wording varies across claude versions).
- **gemini**: `gemini --yolo --skip-trust -i "<body>"` — interactive
  (steerable) with all tool approvals auto-accepted and the
  workspace trust dialog suppressed; exits on `/quit` (the graceful
  kill handshake covers it via its second verb). Headless
  alternative: `-p "<body>"`.
- **aider**: `aider --yes-always --no-check-update --message "<body>"
  [files...]` — headless one-shot, exits clean, and AUTO-COMMITS its
  own edits (no orphan-commit rescues). Model comes from
  ~/.aider.conf.yml (this machine: DeepSeek-only by owner directive —
  do NOT pass --model); requires DEEPSEEK_API_KEY in the environment.
  Litter caveat: it creates/edits .gitignore and .aider* files in the
  sandbox — never stage those. Interactive mode exits on /exit.
- **opencode**: `opencode run "<body>"` — streams live, exits clean,
  and DOES ingest `weave say` lines mid-run (steerable while
  headless). Two caveats: its own permission system auto-rejects
  file-tool reads that resolve through symlinks to outside the
  project — tell it to use bash cat/grep for linked corpora — and a
  rejected tool call can end the run with EXIT 0 and no deliverable:
  always check for the artifact, never trust the exit code alone.
  CONTAINMENT WARNING: opencode keeps persistent per-project state;
  if it has ever worked in the origin repo it gravitates back to it
  by absolute path — even with the sandbox's `origin` remote removed
  (observed twice: committed to the real checkout's master, sandbox
  branch left empty). Prefer codex/claude when sandbox containment
  matters; if you do use opencode, check `git -C <sandbox> log` AND
  the origin repo's HEAD at completion before trusting state.

Background each start (`&`); the wrapper auto-setsids.

## Phase 4 — Monitor

    ycode weave list             # TOOL + STARTED + DUR: who works what, since when
    ycode weave log N -f         # live PTY capture (any number of watchers)
    ycode weave list --watch --json   # NDJSON state transitions

Never measure benchmarks/suites on the host while subagents compile
in parallel — per-test timeouts flake under load and read as
regressions.

BLOCKED-AGENT PROTOCOL — TUI agents stall on dialogs, and a status
question typed into a menu is noise. Watch for waiting-screens in
the capture tail (selection menus `● 1.`/`❯ 1.`, "Enter to
confirm", y/n, `[y]`-style prompts, "Press enter", usage/rate
limits, auth prompts, idle composer). Screen text alone can be
SCROLLBACK — a rendered pane from a command that already finished
(one orchestrator answered a `patch … Assume -R? [y]` prompt that
had been dead for minutes, typing noise into the composer). VERIFY
the block is live before answering:

    ps -axo pid,ppid,stat,command | awk -v wp=<wrapper_pid> '$2==wp'
    # then walk children: a LIVE block shows a child process
    # (patch, a shell, an editor) sitting in a foreground TTY wait;
    # get wrapper_pid from `weave list --json`.

Live block confirmed — or a top-level dialog with NO spinner and no
running children (trust dialogs, limit menus) — then respond to
WHAT THE SCREEN ASKS:

- Orchestrator handles automatically: workspace-trust dialogs
  (`say N "1"`), model-switch/limit menus where one option clearly
  continues the task, composer nudges (Tab to queue, Enter), any
  confirmation consistent with the issue body. (A gemini run once
  sat 30 minutes on a "limit reached — switch model?" menu; the
  answer was `say N "1"`.)
- Escalate to the human, never auto-answer: authentication/login,
  API keys, billing/upgrade decisions, anything destructive or
  outside the issue's scope, and any dialog you can't classify.
  Surface the captured lines verbatim when asking.

## Phase 5 — Steer (weave say)

    ycode weave say N "btw, status check: one line — current measured
    number and what you're working on — then continue."

- One say = one typed line + Enter. Injected text is keystrokes; the
  agent treats it as user input.
- Text injected while nothing reads it lands in the composer and is
  delivered as the NEXT user message — harmless; instruct workers to
  ignore stray one-liners.
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
- **WARNING**: resumed sandboxes keep their ORIGINAL base commit — work
  resumed after sibling merges lands on a stale base; rebase in the sandbox
  or expect conflicts at pull.

## Phase 7 — Converge and verify

    ycode weave wait --all --timeout 50m
    ycode weave pull                      # merges working/submitted branches

- **Check for a dirty sandbox BEFORE pull**: `git -C <sandbox> status
  --porcelain` (ignore `.aider*`/`GEMINI.md` litter). The wrapper's
  verify runs against the WORKING TREE, but pull merges only COMMITS —
  an agent that commits an interim state and keeps improving
  uncommitted gets a green gate attesting work that never merges, and
  sandbox cleanup destroys it (this shipped a silently-regressed
  master once: gate said redir=0, merged commit measured redir=4).
  If dirty with real changes: resume the agent with "commit your
  work", or commit it yourself in the sandbox, and re-verify.

- Steerable TUI runs don't exit on DONE. `weave kill N` now tries a
  graceful `/exit`//`/quit` handshake first — a tool that obeys
  exits 0 and the wrapper records a genuine `submitted` (then
  `pull` merges it normally). Verify first when in doubt:
  `weave say N "btw, are you done? reply DONE"` and watch the log.
  Only a non-responding tool gets force-killed, recorded as
  `killed` (its own state — never promoted) with wrapper-measured
  evidence (`commits_ahead`, `head`) on the item; inspect, then
  resume or merge deliberately:
      git fetch --no-tags <sandbox> agent/weave-issue-N:agent/weave-issue-N
      git merge --no-ff agent/weave-issue-N
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

For multi-issue rounds, finish with a JUDGE pass (the runbook's
Pattern B): after merging, file one more issue on a fresh sandbox
off the merged state — independently re-measure every claim, run
the full suite, audit each commit for test deletions/scope spills,
and commit a verdict report. A judge round caught nothing the
orchestrator missed exactly once so far; every other time it
surfaced a reconciliation worth recording.

## Failure modes, condensed

| Symptom | Likely cause | Move |
|---|---|---|
| `weave list` empty | wrong cwd | follow the hint; cd to the repo |
| capture empty, agent "working" | tool buffers (claude -p) | read its transcript; next time use streaming mode |
| agent cites paths outside sandbox | corpus missing in clone, or remote followed | Phase 2 links; origin is removed from sandboxes — verify `git remote` is empty |
| claimed improvement won't reproduce | agent measured differently | re-run the issue's exact REPRO; park the branch |
| state=failed after a kill you issued | kill bookkeeping | branch is intact; manual fetch+merge |
| exit 143, no commits | watchdog kill | check sandbox for uncommitted progress before re-filing |

---
topic: weave
summary: orchestrate parallel subagents via a queue + worktrees
when: you (as orchestrator) want to file issues, fan out subagent CLIs against them, then merge results back
audience: agent
max_lines: 200
---

`ycode weave` is the v2 front door over the loom substrate, designed
for the orchestrator-fans-out pattern: you (claude-code, codex,
opencode, gemini) file a queue of issues, launch one subagent CLI
per issue inside an isolated git worktree, wait for them to finish,
then merge the converged work back into main.

The whole surface is a local-only backend: queue.json under
`$HOME/.agents/ycode/weave/<repo-tag>/`, one **full git clone** per
issue (each sandbox has its own `.git/` — refs cannot cross the
boundary, the user's repo is untouched until you `weave pull`),
plain `git merge --no-ff` for convergence. No Gitea, no `ycode
serve` needed.

## When to use this

- You're an orchestrator agent and need to delegate N independent
  pieces of work to peer subagents (codex, claude-code, opencode,
  aider, gemini) and merge their results back.
- You want each subagent to edit files without stomping on the
  others (independent git worktrees per issue).
- You want a deterministic merge step (`weave pull`) instead of
  ad-hoc `git cherry-pick` orchestration.

## Subagent stdio: PTY by default

The subagent runs inside a freshly-allocated PTY so TUI agents
(claude-code, codex, opencode, aider) render correctly. When you
(the orchestrator) launch `weave start` via a non-TTY parent (your
Bash tool, or `&` in a script), the PTY output is captured to a
per-issue log file under the queue dir — see `log_path` in the
result envelope. When a human runs it from a real terminal, the PTY
passes through interactively. Either way the subagent sees a real
TTY on stdin/stdout/stderr.

If you genuinely want pipe semantics (subagent isn't a TUI), pass
`--pty=never`.

## Orchestrator patterns

Two shapes; pick by whether you need a post-merge validation gate.

**(A) Parallel impl only.** `add` → background N `start`s →
`wait --all` → `pull`.

**(B) Parallel impl + judge validation.** A separate agent
(typically opencode) validates the merged state and either signs
off or commits a diagnosis. The validation issue MUST be added
*after* `pull` — otherwise its sandbox clones a stale main. Use a
judge agent rather than per-implementer self-validation because the
implementer only sees its own branch and agents grading their own
work tend to narrow tests when stuck.

`weave` has no `--depends-on` today; express the DAG via `wait`.
Each `start` blocks; background with `&` (ycode auto-`setsid`s so
they survive the shell's exit).

## States

- **todo** — added, not yet claimed.
- **working** — `weave start` claimed it; subagent is running.
- **submitted** — subagent exited 0; branch is ready for `pull`.
- **failed** — subagent exited non-zero. `exit_code` and `log_path`
  on the queue item; `pull` skips these.
- **done** — `pull` merged the branch into main; worktree torn down.
- **abandoned** — `weave abandon <issue>` was called; sandbox and
  branch removed, queue item kept for history.

`weave pull` accepts both `working` and `submitted`; `submitted` is
the normal path when subagents have finished cleanly.

## Subverbs

- `add "<title>" [--priority p0|p1|p2|p3] [--body] [--from-file]` —
  file an issue; `--from-file` parses a markdown checklist
  (`- [ ] title`) or JSON list of `{title,body,priority}`.
- `list [--history] [--watch]` — show active (and with `--history`,
  reaped/abandoned) issues. `--watch` streams NDJSON state
  transitions.
- `next` — peek at top-of-queue without claiming.
- `prio <issue> <p0|p1|p2|p3>` — change priority.
- `start [--issue N | top-of-queue] [--no-spawn] [--resume]
  [--pty=auto|always|never] [--idle-timeout DUR] -- <tool> [args...]` —
  claim, allocate sandbox clone, exec tool. On exit: state =
  submitted (rc=0) or failed (rc≠0). `--idle-timeout` SIGTERMs the
  subagent if no PTY output appears for that long (e.g. `5m`);
  default off. Useful for TUI agents that have no built-in auto-
  exit and can hang silently mid-task.
- `wait [--issue N | --all] [--timeout DUR]` — block until target
  reaches terminal state.
- `pull` — merge every working/submitted branch with commits ahead.
- `abandon <issue> [--reason]` — stop running wrapper + tear down
  sandbox + branch. Use when giving up entirely.
- `kill <issue> [--reason]` — stop the running wrapper PRECISELY
  via its recorded PID + setsid process group, keep sandbox +
  branch + partial commits. Use when the subagent is stuck and
  you want to inspect / resume. NEVER use `pkill` / `killall` /
  `kill -9` to stop a subagent — pattern matchers also catch peer
  ycode / claude / codex sessions belonging to other agents on the
  same machine.
- `shell <issue>` — drop into `$SHELL` inside the worktree (in
  agent mode returns the sandbox path as JSON instead).
- `reset [--yes]` — tear down every weave + clear queue.
- `open`, `init-board` — Gitea-backed; emit `dependency_unhealthy`
  on the local-only backend.

## Envelope contract

Every subverb supports `--json` and, when `YCODE_AGENT=1` is set,
defaults to JSON. The envelope is `{schema_version: "loom-v2",
command, status: ok|error, result?: {...}, error?: {code, message}}`.
Stable exit codes: 0 ok / 2 invalid_arg / 3 precondition_failed /
4 state_conflict / 5 dependency_unhealthy.

## Failure modes

| Symptom | Fix |
|---|---|
| `weave start` exits with `tool exited with N` | Subagent failed. Read the log at `log_path`, fix the prompt, `weave abandon <issue>`, re-`add` + re-`start`. |
| `weave wait --all` times out | Some item is still working or todo. Inspect `weave list --history` to see what's stuck; the orchestrator may need to re-start a crashed subagent. |
| `weave pull` says `conflict` | The branch and main diverged. Resolve manually in the worktree (`weave shell <issue>`), commit, retry `pull`. |
| Subagent renders garbage / can't read stdin | PTY allocation failed or you passed `--pty=never`. Drop `--pty=never`. |
| Backgrounded `weave start` dies when shell exits | Should not happen — ycode auto-`setsid`s on non-TTY. File a bug with reproduction. |

## Exact calls

### Pattern A — parallel impl only

```bash
ycode weave add "fix #1" --priority p0 --json
ycode weave add "fix #2" --priority p0 --json

ycode weave start --issue 1 -- codex       "fix #1" &
ycode weave start --issue 2 -- claude-code "fix #2" &

ycode weave wait --all --timeout 30m --json
ycode weave pull --json
# you're on main with both merge commits; `make test && git push` is yours
```

### Pattern B — parallel impl + judge validation

```bash
# Phase 1: file + run the implementers in parallel.
ycode weave add "fix #1" --priority p0 --json
ycode weave add "fix #2" --priority p0 --json
ycode weave start --issue 1 -- codex       "fix #1" &
ycode weave start --issue 2 -- claude-code "fix #2" &
ycode weave wait --all --timeout 30m --json

# Phase 2: inspect failures BEFORE merging. weave pull skips
# state=failed branches, but you usually want a decision (abandon
# + re-file, or `start --resume --issue N` after fixing the prompt)
# rather than a silent skip.
ycode weave list --history --json
# … decide / re-file / re-start as needed …

# Phase 3: merge clean branches.
ycode weave pull --json

# Phase 4: NOW file the validation issue (after pull, so the
# judge's worktree branches off the merged main).
ycode weave add "validate + run e2e" --priority p1 --json
# capture the new ID from the envelope; assume it's 3.

# Phase 5: run the judge against the merged state. Foreground is
# fine here (only one thing running) or background + wait.
ycode weave start --issue 3 -- opencode "$(cat <<'EOF'
Run the full e2e suite against main. If everything passes, commit
an empty marker file ./.judge-pass and exit 0. If anything fails,
write your diagnosis to ./.judge-report.md, commit it, exit 0.
Either way your branch will be merged so the orchestrator can
read the marker / report.
EOF
)"

# Phase 6: merge the judge's commit (carries pass-marker OR
# diagnosis — both are useful in history).
ycode weave pull --json
# Inspect ./.judge-pass / ./.judge-report.md on main; proceed
# to `make test && git push` only if the gate signals green.
```

If the judge reports failures, file new fix issues, re-run the
phases. weave's queue persists across orchestrator turns — a
follow-up `weave add` lands at the next NextID, and the prior
items keep their state for history.

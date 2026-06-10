---
topic: weave
summary: orchestrate parallel subagents via a queue + worktrees
when: you (as orchestrator) want to file issues, fan out subagent CLIs against them, then merge results back
audience: agent
max_lines: 140
---

`ycode weave` is the v2 front door over the loom substrate, designed
for the orchestrator-fans-out pattern: you (claude-code, codex,
opencode, gemini) file a queue of issues, launch one subagent CLI
per issue inside an isolated git worktree, wait for them to finish,
then merge the converged work back into main.

The whole surface is a local-only backend: queue.json under
`$HOME/.agents/ycode/weave/<repo-tag>/`, one git worktree per issue,
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

## The MVP orchestrator flow

`add` → background N `start`s → `wait --all` → `pull`. Each `start`
blocks until its subagent exits, so background them with `&`.
ycode auto-`setsid`s when stdin is non-TTY, so they survive the
launching shell's exit without `nohup`/`disown`. `wait` exit codes:
0 ok / 3 timeout / 2 invalid_arg. After `pull` you're on main with
the merge commits; the final `make test` / `git push` is yours.

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
  [--pty=auto|always|never] -- <tool> [args...]` — claim, allocate
  worktree, exec tool. On exit: state = submitted (rc=0) or
  failed (rc≠0).
- `wait [--issue N | --all] [--timeout DUR]` — block until target
  reaches terminal state.
- `pull` — merge every working/submitted branch with commits ahead.
- `abandon <issue> [--reason]` — tear down worktree + branch.
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

```bash
# Plan
ycode weave add "fix null deref" --priority p0 --json
ycode weave add --from-file backlog.md --priority p2 --json

# Fan out (background each; auto-PTY + auto-setsid mean they
# survive your launching shell and render correctly even with
# pipe-based stdio).
ycode weave start --issue 1 -- codex "fix #1" &
ycode weave start --issue 2 -- claude-code "fix #2" &
ycode weave start --issue 3 -- opencode "validate + run e2e" &

# Wait until every working/todo item reaches a terminal state.
ycode weave wait --all --timeout 30m --json

# Merge submitted branches into main.
ycode weave pull --json

# Inspect / clean up.
ycode weave list --history --json
ycode weave abandon 4 --reason "blocked" --json
ycode weave reset --yes --json
```

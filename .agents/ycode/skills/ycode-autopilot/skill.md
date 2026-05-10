---
name: autopilot
description: Autonomously execute a development task — fix, improve, or arbitrary goal — through a research-plan-build-test-fix-commit loop
user_invocable: true
---

# /autopilot — Autonomous Development Task Loop

`/autopilot` runs a development task end-to-end without stopping for
approval: research the relevant code, write a short plan, implement,
test, fix until green, and commit. It is for tasks that don't already
have a narrow skill.

`{{ARGS}}` selects a **mode** as its first word, followed by the task
description. If `{{ARGS}}` is empty, ask the user **once** what the task is, then run to completion.

## When NOT to use this skill

For tasks that already have a tight, single-purpose skill, invoke that one directly:

| Goal | Use instead |
| --- | --- |
| Just compile and fix build errors | `/build` |
| Just run integration / smoke tests | `/validate` |
| Just deploy to localhost or remote | `/deploy` |
| Compare ycode against a third-party agentic tool, gap-analyze, implement | `/analyze` |
| Research a project or topic without implementing (gap analysis only) | `/learn` |
| Run aperio benchmarks against other agents | `/eval` |

## Modes

| Mode | ARGS shape | Purpose |
|------|-----------|---------|
| `fix <issue>` | issue description in plain English | Diagnose and fix a specific bug, failure, or regression |
| `improve <area>` | code area or aspect to improve | Research, propose enhancements, implement the highest-leverage ones |
| `task <goal>` | free-form development goal | Generic catch-all: feature work, refactor, cleanup, anything else |

If `{{ARGS}}` doesn't begin with a recognized mode keyword, treat the
whole input as `task <goal>`.

---

## Shared backbone

Every mode walks the same eight steps. Skip steps when the mode-specific
guidance below says they're optional.

### Step 1: UNDERSTAND

Parse `{{ARGS}}` into a mode and a one-line goal. Derive 1–3
acceptance criteria from the goal — what specifically must be true
when this is done?

If the goal is genuinely ambiguous in a way that changes the
approach (not just lacks detail), ask **one** focused clarifying
question. Otherwise, proceed.

### Step 2: RESEARCH

Use `Read`, `Bash` (`grep`, `find`), and existing search tools to
explore the relevant code. Identify:

- The files and functions involved in the task
- Existing utilities and patterns to **reuse** rather than reimplement
- Prior art elsewhere in the codebase

For `improve` and `task`, this step is mandatory. For `fix` of a
trivial reproducible bug, it can be brief.

### Step 3: PLAN

Write a short plan-of-attack (in conversation context, or as a
temporary file under `.claude/plans/` for non-trivial work). Include:

- Files to create or modify (with paths)
- Functions to reuse with their file paths and line numbers
- Test/verification approach

Keep it scannable. The plan is for execution, not documentation.

### Step 4: BUILD

Implement the plan using `Read` / `Edit` / `Write`. Stay tight:

- Don't refactor surrounding code unless the plan calls for it
- Don't add features beyond the goal
- Reuse functions and patterns identified in RESEARCH

### Step 5: TEST

Run `make build` (full quality gate: tidy → fmt → vet → compile →
test → verify).

For changes that touch areas covered by `/validate`, additionally run
the relevant `make validate*` target. Don't reinvent the test pipeline
— if `/build`'s flow fits, follow it; if `/validate`'s does, follow that.

### Step 6: FIX

If TEST fails, examine the error, fix the **root cause** (not the
test, unless the test itself is wrong), then re-run TEST.

Cap at **3 fix-and-retry cycles** per task. If still failing, stop and
report the unresolved error to the user — do not commit partial work.

### Step 7: COMMIT

Once the build is green:

1. `git status` to see what changed.
2. Stage modified files **by name** — never `git add -A` or `git add .`.
3. Commit with a message that follows the repo's prefix convention
   (`fix:`, `feat:`, `refactor:`, `chore:`, `docs:`, `test:`) and
   explains the **why** in 1–2 sentences. Match the style of recent
   commits via `git log --oneline -10`.
4. Do not push unless `--push` appears in `{{ARGS}}`.

If there are zero changes (no-op task), skip the commit and say so in
the summary.

### Step 8: SUMMARIZE

Final report to the user, in 3–5 lines:
- What changed (1 sentence)
- Files touched and the commit hash
- What's verified (build green, tests passed, etc.)
- Any unresolved follow-ups or caveats

---

## Mode-specific guidance

### `fix <issue>`

- **RESEARCH is the highest-leverage step.** Before changing code,
  confirm you understand the root cause. Try to reproduce the issue if
  possible. If the issue mentions a specific failure mode, find the
  code path that produces it.
- **PLAN can be brief** — often a single sentence ("change X in `foo.go`
  to handle Y") and the file/line.
- **Prefer the smallest fix** that addresses the reported issue. Don't
  bundle unrelated cleanups.

### `improve <area>`

- **PLAN must enumerate candidates.** List 3–5 possible improvements,
  pick the highest-leverage 1–2, and explain why the rest are
  deferred. Don't try to do every improvement in one pass.
- **Match scope to a single coherent commit.** If the chosen
  improvements span unrelated changes, narrow further.
- **Be honest about value.** If RESEARCH reveals the area is already
  well-engineered, say so and exit with no commit.

### `task <goal>`

- **Most open-ended; structure is your responsibility.**
- If the goal naturally decomposes into 1–3 sub-tasks, do that in PLAN
  and walk BUILD/TEST/FIX/COMMIT once per sub-task with a separate
  commit each. For larger decompositions, recommend `ycode sprint`
  instead.
- For tightly-scoped feature work that fits a single commit, treat it
  the same as `improve` — research, plan, implement, verify, commit.

---

## Rules

- **Fully autonomous.** Never ask for confirmation between steps;
  proceed once the task is parsed.
- **Never modify** anything under `priorart/` or `external/` —
  read-only.
- **Stage by name** — never `git add -A` or `git add .`.
- **`make build` must pass** before any commit.
- **No push** unless `--push` appears in `{{ARGS}}`.
- **Cap retries.** 3 fix-and-retry cycles per task; report and stop if
  still red.
- **Reuse, don't reinvent.** If `/build`, `/validate`, or `/deploy`
  already cover a step, defer to their procedure.

---

## Collab mode

When `{{ARGS}}` starts with `collab`, the agent is one of N workers
operating against ycode's internal Gitea (see
[docs/agent-collab.md](../../docs/agent-collab.md)). This mode is
typically invoked by `ycode autopilot collab --agents N`, not by humans.

Differences from default mode:

- **Workspace is a fork checkout, not cwd.** The orchestrator hands the
  agent a temp dir already cloned from `admin/<slug>` in internal Gitea
  with HEAD on a fresh `agent/<id>/issue-<N>` branch.
- **Task comes from the queue.** The orchestrator pops the highest-priority
  unclaimed issue from `tasks/<slug>` and passes title+body as the goal.
- **Commit author is `agent-<id>`** — the orchestrator pre-configures
  `user.email` / `user.name` in the worktree; do not override.
- **Push, then PR.** After `make build` is green and you've committed,
  push the branch (the orchestrator pre-configures the `ycode-internal`
  remote) and open a PR back to `main`. The merger handles auto-merge.
- **Never touch cwd.** Treat the user's working tree as off-limits.
  The orchestrator's pull command (`ycode tasks pull`) is the only
  channel that sync's merged work back to cwd.

If a step fails and you can't recover, call `Release` on the issue
(deferred tool: search for `tasks_release`) so another agent can pick
it up; do not silently abandon.

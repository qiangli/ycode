---
name: commit
description: Plan and commit local changes with a well-scoped, convention-following commit message
user_invocable: true
---

# /commit — Commit Local Changes

Analyze uncommitted changes, draft an appropriate commit message following
[Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/),
and create a clean git commit.

{{ARGS}}

## Step 1: Gather context

Run these commands **in parallel** using Bash:

1. `git status` — identify modified, staged, or untracked files. Do not use `-uall`.
2. `git diff` and `git diff --cached` — read unstaged and staged changes.
3. `git log --oneline -5` — observe the repo's commit message style.

## Step 2: Analyze and draft

1. **Identify scope.** Determine which changed files belong to the current
   logical change. Compare against the initial git status snapshot from the
   system prompt to distinguish pre-existing dirty files from changes made
   during this session — do not stage pre-existing changes. Exclude files
   containing secrets (`.env`, credentials, tokens).

2. **Classify the change.** Match the convention observed from `git log`:
   - `fix:` — a bug fix
   - `feat:` — a new feature
   - `docs:` — documentation only
   - `refactor:` — restructuring without behavior change
   - `test:` — adding or updating tests
   - `chore:` — maintenance, dependencies, config

3. **Draft the commit message.** Keep it concise (1-2 sentences). Focus on
   **why** the change was made. Use the same tense and style as recent commits.

## Step 3: Stage and commit

1. **Stage specific files by name.** Use `git add <file1> <file2>` — never
   `git add -A` or `git add .`.

2. **Create the commit** via HEREDOC:

   ```bash
   git commit -m "$(cat <<'EOF'
   <type>: <short summary>

   <optional body explaining why>
   EOF
   )"
   ```

3. **Verify.** Run `git status` after the commit.

## Step 4: Handle hook failures

If the commit fails due to a **pre-commit hook**:

1. Read the hook's error output.
2. Fix the underlying issue — do not bypass with `--no-verify`.
3. Re-stage fixed files with `git add <file>`.
4. Create a **new** commit — do not use `--amend`.

Stop after 3 fix-and-retry cycles if the hook still fails.

## Rules

- Do not amend unless the user explicitly asks.
- Do not push unless the user explicitly asks.
- Do not skip hooks (`--no-verify`) unless the user explicitly asks.
- Do not use interactive git flags (`-i`).
- If there are no changes to commit, report that and stop.
- If pre-existing dirty files are unrelated, leave them alone.

## On success

Report: short commit hash, commit message, any remaining uncommitted files.

## On failure

Report the exact error output and what was attempted.

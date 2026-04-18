---
name: commit
description: Plan and commit local changes with a well-scoped, convention-following commit message
user_invocable: true
---

# /commit — Commit Local Changes

Analyze uncommitted changes, draft an appropriate commit message following [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/), and create a clean git commit.

## Instructions

### Step 1: Gather context

Run these three commands **in parallel** to gather context:

1. `git status` — identify which files have been modified, staged, or are untracked. Do not use the `-uall` flag.
2. `git diff` and `git diff --cached` — read the actual content of unstaged and staged changes respectively.
3. `git log --oneline -5` — observe the repository's commit message style (prefix conventions, tense, length).

### Step 2: Analyze and draft

1. **Identify scope.** Determine which changed files belong to the current logical change. Compare the current `git status` against the initial git status snapshot from the system prompt to distinguish pre-existing dirty files from changes made during this session — do not stage pre-existing changes. Exclude files that contain secrets (`.env`, credentials, tokens) — warn the user if they ask to commit those.

2. **Classify the change.** Match the convention observed from `git log`:
   - `fix:` — a bug fix
   - `feat:` — a new feature
   - `docs:` — documentation only
   - `refactor:` — restructuring without behavior change
   - `test:` — adding or updating tests

3. **Draft the commit message.** Keep it concise (1-2 sentences). Focus on **why** the change was made, not a mechanical list of what changed. Use the same tense and style as recent commits in the repo.

### Step 3: Stage and commit

1. **Stage specific files by name.** Use `git add <file1> <file2>` — do not use `git add -A` or `git add .`, which can accidentally include unrelated files, secrets, or large binaries.

2. **Create the commit.** Pass the message via a HEREDOC to handle special characters and multi-line bodies:

   ```bash
   git commit -m "$(cat <<'EOF'
   <type>: <short summary>

   <optional body explaining why>
   EOF
   )"
   ```

3. **Verify.** Run `git status` after the commit to confirm it succeeded and no unintended files were left behind.

### Step 4: Handle hook failures

If the commit fails due to a **pre-commit hook**:

1. Read the hook's error output to understand what failed (lint, formatting, tests).
2. Fix the underlying issue — do not bypass with `--no-verify`.
3. Re-stage the fixed files with `git add <file>`.
4. Create a **new** commit — do not use `--amend`, because the failed commit did not happen and amending would modify the *previous* commit.

If after 3 fix-and-retry cycles the hook still fails, stop and report the unresolved error to the user.

## Rules

- Do not amend an existing commit unless the user explicitly asks.
- Do not push to a remote unless the user explicitly asks.
- Do not skip hooks (`--no-verify`, `--no-gpg-sign`) unless the user explicitly asks.
- Do not use interactive git flags (`-i`) — use non-interactive equivalents.
- If there are no changes to commit, report that and stop.
- If pre-existing dirty files are unrelated to the current work, leave them alone and note their presence.

## On success

Report: the short commit hash, the commit message, and any remaining uncommitted files.

## On failure

Report the exact error output and what was attempted. Do not commit partial fixes.

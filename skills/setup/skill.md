---
name: setup
description: Set up the ycode development environment and verify the build
user_invocable: true
---

# /setup — Set Up ycode for Development

Prepare the ycode project for local development. This skill is specific to
the ycode codebase — for initializing third-party repos, use `/init`.

{{ARGS}}

## Step 1: Verify prerequisites

Run these checks **in parallel**:

1. `go version` — confirm Go 1.26+ is installed
2. `git status` — confirm this is a git repository
3. `make compile` — verify the project compiles

If any prerequisite fails, report what's missing and stop.

## Step 2: Run the full build

```bash
make build
```

This runs: tidy → fmt → vet → compile → test → verify. Fix any failures
before proceeding.

## Step 3: Initialize workspace artifacts

Run `/init` to create `.agents/ycode/`, `.agents/ycode.json`, `.gitignore`
entries, and AGENTS.md if they don't already exist.

## Step 4: Report

Summarize:
- Go version and platform
- Build status (pass/fail)
- Which workspace artifacts were created or already existed
- Any issues that need attention

---
name: build
description: Build ycode binary with full quality checks, fix errors, and commit on success
user_invocable: true
---

# /build — Build ycode

Build the ycode binary from source with full quality gate checks. Fix any errors encountered. Commit changes on success.

## Instructions

Work in the project root. The goal is to make `make build` pass. If it fails, diagnose and fix the error, then retry. Repeat until it succeeds or the problem is beyond automatic repair (in which case, report to the user and stop).

### Step 1: Run the build

```bash
make build
```

This runs: `go mod tidy` → `go fmt` → `go vet` → `go build` → `go test -race` → `bin/ycode version`.

### Step 2: If build fails — fix and retry

Examine the error output. Common fixable issues:

- **Formatting**: Already handled by `go fmt` in the pipeline.
- **Vet warnings / compile errors**: Read the offending file, understand the issue, and apply the fix using the Edit tool.
- **Test failures**: Read the failing test and the code under test. Fix the root cause (not the test, unless the test itself is wrong).
- **Missing dependencies**: Run `go mod tidy` or `go get` as needed.

After applying a fix, re-run `make build` from the top. Do NOT skip steps — the full pipeline must pass end-to-end.

If after 3 fix-and-retry cycles the build still fails, stop and report the unresolved error to the user.

### Step 3: Commit on success

Once `make build` succeeds with exit code 0:

1. Check `git status` for any changed files (fixes applied, formatting, go.sum updates, etc.).
2. If there are changes, stage and commit them with a descriptive message summarizing what was fixed or changed.
3. If there are no changes (clean build on first try), skip the commit.

### On success

Report: binary path, version, and whether a commit was created.

### On failure (after retries exhausted)

Report the exact error output and what was attempted. Do NOT commit partial fixes.

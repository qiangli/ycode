---
name: validate
description: Run integration, smoke, acceptance, and performance tests against a running ycode instance
user_invocable: true
---

# /validate — Validate a running ycode instance

Run the full validation test suite against a running ycode server. This covers integration, smoke, acceptance, and performance tests. Unit tests are excluded (handled by `/build`).

## Arguments

- `/validate` — validate against localhost:58080 (default)
- `/validate <host>:<port>` — validate against a specific endpoint

Parse the argument to determine **BASE_URL**. Default: `http://localhost:58080`

## Instructions

### Pre-flight: Ensure build and deploy

1. **Build**: Run the `/build` skill to ensure the binary compiles, tests pass, and changes are committed.
2. **Deploy**: Run the `/deploy` skill to ensure the server is running at the target endpoint.

If either skill fails, stop and report the failure. Do NOT run validation against a broken or non-running instance.

### Run validation

Once build and deploy succeed, run:

```bash
make validate
```

With appropriate HOST/PORT if non-default:
```bash
make validate HOST=<host> PORT=<port>
```

### If `make validate` fails

Examine the failure output:

- **Connectivity failure** ("No server reachable"): The deploy didn't work. Re-run `/deploy` and retry.
- **Smoke/integration test failures**: These indicate a bug in the server code. Read the error, identify the root cause in the source, fix it, then re-run `/build` followed by `/deploy` followed by `make validate`.
- **Acceptance test failures**: Same as above — fix, build, deploy, re-validate.
- **Performance test failures**: These are informational and should not cause a hard failure. Report the numbers.

After applying a fix, repeat the full cycle: `/build` → `/deploy` → `make validate`. Do NOT skip steps.

If after 3 fix-and-retry cycles validation still fails, stop and report the unresolved failures to the user.

### On success

Report the full validation summary table from `make validate` output, including pass/fail/skip counts and performance baselines.

### On failure (after retries exhausted)

Report which tests failed, what was attempted, and the remaining error output.

---
name: validate
description: Run integration, smoke, acceptance, and performance tests against a running ycode instance
user_invocable: true
---

# /validate — Validate a running ycode instance

Run the full validation test suite against a running ycode server. All tests are written in Go (`internal/integration/`) using the `integration` build tag and test against real services.

## Test Suites

| Suite | File | What it tests |
|-------|------|---------------|
| Smoke | `smoke_test.go` | Health endpoint, CLI version, server status |
| Proxy | `proxy_test.go` | Landing page discovery, all proxied app routes reachable |
| OTEL | `otel_test.go` | Real OTEL collector (traces, metrics, logs), Prometheus endpoint |
| Acceptance | `acceptance_test.go` | One-shot prompt, serve status, doctor check |
| Performance | `perf_test.go` | Health latency (p50/p95/p99), trace throughput, binary startup |

## Arguments

- `/validate` — validate against localhost:58080 (default)
- `/validate <host>:<port>` — validate against a specific endpoint

Parse the argument to determine **HOST** and **PORT**. Default: `localhost:58080`

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

This runs `go test -tags integration -v -count=1 ./internal/integration/...` with the appropriate environment variables.

### If `make validate` fails

Examine the Go test output:

- **Connectivity skip** ("server not reachable"): The deploy didn't work. Re-run `/deploy` and retry.
- **TestProxyApps failures**: Check component status and proxy registration in `internal/observability/stack.go`.
- **TestOTEL failures**: OTEL collector or Prometheus may not be running. Check `internal/observability/` component startup.
- **TestAcceptance failures**: These indicate a bug in the server or CLI code. Read the error, identify the root cause, fix it, then re-run `/build` → `/deploy` → `make validate`.
- **TestPerformance warnings**: Performance tests log warnings but don't hard-fail. Report the numbers.

After applying a fix, repeat the full cycle: `/build` → `/deploy` → `make validate`. Do NOT skip steps.

If after 3 fix-and-retry cycles validation still fails, stop and report the unresolved failures to the user.

### On success

Report the full `go test -v` output showing pass/skip/fail for each test function, plus any logged performance metrics.

### On failure (after retries exhausted)

Report which tests failed, what was attempted, and the remaining error output.

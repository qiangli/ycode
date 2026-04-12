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
| Proxy | `proxy_test.go` | Landing page discovery, all proxied app routes reachable, real UI content verification, health endpoints |
| OTEL | `otel_test.go` | Real OTEL collector (traces, metrics, logs), Prometheus exporter endpoint |
| Acceptance | `acceptance_test.go` | One-shot prompt, serve status, doctor check |
| Performance | `perf_test.go` | Health latency (p50/p95/p99), trace throughput, binary startup |

### Proxy test details

The proxy suite (`proxy_test.go`) includes three test functions:

- **TestProxyApps**: Discovers links from the landing page and verifies all routes return 200/301/302.
- **TestProxyAppUIContent**: Verifies each third-party app serves its **real UI** (not a placeholder). Checks for characteristic content markers and ensures placeholder markers are absent. Each app has a known marker:
  - `/prometheus/` — contains `<title>Prometheus</title>`, must NOT contain `ycode Prometheus`
  - `/alerts/` — contains `script.js`, must NOT contain `ycode Alerts`
  - `/dashboard/` — contains `Perses`
  - `/logs/` — follows redirect to vmui, contains `VictoriaLogs`
  - `/traces/` — contains `Jaeger`, must NOT contain `This is not the Jaeger UI`
  - `/collector/` — contains `"status"`
- **TestProxyAppHealthEndpoints**: Checks `/prometheus/-/healthy`, `/alerts/-/healthy`, `/dashboard/api/v1/health`, and `/healthz`.

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
- **TestProxyApps failures**: Check component status and proxy registration in `internal/observability/stack.go`. Verify each component's `Start()` succeeded in the serve log (`~/.ycode/observability/serve.log`).
- **TestProxyAppUIContent failures**: A proxied app is returning placeholder HTML instead of its real UI. Common causes and fixes:
  - **Prometheus** (`ycode Prometheus` placeholder): Check that `prometheus.go` returns `nil` from `HTTPHandler()` and exposes `Port()`, and that `static/prometheus/index.html` exists.
  - **Alertmanager** (`ycode Alerts` placeholder): Check that `alertmanager.go` imports and serves `asset.Assets` from `github.com/prometheus/alertmanager/asset`.
  - **Jaeger** (`This is not the Jaeger UI`): The Jaeger UI assets are not embedded. Ensure `external/jaeger/cmd/jaeger/internal/extension/jaegerquery/internal/ui/actual/` contains gzipped UI files (run the Jaeger `rebuild-ui.sh` script or download from GitHub releases).
  - **Perses** (500 error): Check `external/perses/ui/embed_stub.go` embeds `app/dist` (built React UI). If missing, build with `cd external/perses/ui && npm install && npx turbo run build --filter=@perses-dev/app`.
  - **VictoriaLogs** (400 at root): Check that `victorialogs.go` redirects `/` to the vmui path.
- **TestProxyAppHealthEndpoints failures**: A component's health endpoint is not reachable through the proxy. Check component startup logs and proxy route registration.
- **TestOTEL failures**: OTEL collector may not be running. Check the serve log for collector startup errors. Common issues:
  - `Telemetry must not be nil`: Add `otelconftelemetry.NewFactory()` to collector factories.
  - Port binding conflicts: Ensure `service.telemetry.metrics.level: none` in collector YAML config.
  - Prometheus exporter empty: The `/metrics` endpoint on port 8889 may be empty until metrics are received — this is normal.
- **TestAcceptance failures**: These indicate a bug in the server or CLI code. Read the error, identify the root cause, fix it, then re-run `/build` → `/deploy` → `make validate`.
- **TestPerformance warnings**: Performance tests log warnings but don't hard-fail. Report the numbers.

After applying a fix, repeat the full cycle: `/build` → `/deploy` → `make validate`. Do NOT skip steps.

If after 3 fix-and-retry cycles validation still fails, stop and report the unresolved failures to the user.

### On success

Report the full `go test -v` output showing pass/skip/fail for each test function, plus any logged performance metrics.

### On failure (after retries exhausted)

Report which tests failed, what was attempted, and the remaining error output.

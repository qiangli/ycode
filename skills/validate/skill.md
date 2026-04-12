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

Run each test suite in order. Continue through all suites even if some tests fail — collect all results and report a summary at the end.

### Pre-flight: Connectivity check

```bash
curl -sf --max-time 5 ${BASE_URL}/healthz
```

If this fails, report that no server is reachable at `${BASE_URL}` and suggest running `/deploy` first. **Stop here.**

---

### Suite 1: Smoke Tests

Quick checks that the server is alive and core endpoints respond.

**1.1 Health endpoint**
```bash
curl -sf ${BASE_URL}/healthz
```
Expect: HTTP 200 with a response body.

**1.2 Dashboard reachable**
```bash
curl -sf -o /dev/null -w "%{http_code}" ${BASE_URL}/dashboard/
```
Expect: HTTP 200 or 301/302 redirect.

**1.3 Version via CLI**
If testing localhost, also run:
```bash
bin/ycode version
```
Expect: version string output without error.

**1.4 Server status**
If testing localhost:
```bash
bin/ycode serve status --port ${PORT}
```
Expect: component table with health statuses.

---

### Suite 2: Integration Tests

Test that the observability stack components are functioning together.

**2.1 OTEL Collector health**
```bash
curl -sf http://${HOST}:4318/v1/traces -X POST \
  -H "Content-Type: application/json" \
  -d '{"resourceSpans":[]}' 2>&1
```
Expect: HTTP 200 (empty spans accepted).

**2.2 Prometheus metrics endpoint**
```bash
curl -sf http://${HOST}:8889/metrics | head -5
```
Expect: Prometheus exposition format (lines starting with `#` or metric names).

**2.3 Send a test trace and verify**
```bash
# Send a test span via OTLP/HTTP
curl -sf http://${HOST}:4318/v1/traces -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "resourceSpans": [{
      "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "validate-test"}}]},
      "scopeSpans": [{
        "spans": [{
          "traceId": "00000000000000000000000000000001",
          "spanId": "0000000000000001",
          "name": "validate-smoke",
          "kind": 1,
          "startTimeUnixNano": "'$(date +%s)000000000'",
          "endTimeUnixNano": "'$(( $(date +%s) + 1 ))000000000'"
        }]
      }]
    }]
  }'
```
Expect: HTTP 200.

**2.4 Send test metrics**
```bash
curl -sf http://${HOST}:4318/v1/metrics -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "resourceMetrics": [{
      "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "validate-test"}}]},
      "scopeMetrics": [{
        "metrics": [{
          "name": "validate_test_counter",
          "sum": {
            "dataPoints": [{"asInt": "1", "startTimeUnixNano": "'$(date +%s)000000000'", "timeUnixNano": "'$(date +%s)000000000'"}],
            "isMonotonic": true,
            "aggregationTemporality": 2
          }
        }]
      }]
    }]
  }'
```
Expect: HTTP 200.

**2.5 Send test log**
```bash
curl -sf http://${HOST}:4318/v1/logs -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "resourceLogs": [{
      "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "validate-test"}}]},
      "scopeLogs": [{
        "logRecords": [{
          "timeUnixNano": "'$(date +%s)000000000'",
          "body": {"stringValue": "validation smoke test log entry"},
          "severityText": "INFO"
        }]
      }]
    }]
  }'
```
Expect: HTTP 200.

**2.6 Proxy routing**
Test that the reverse proxy routes to backend components:
```bash
# Jaeger UI via proxy
curl -sf -o /dev/null -w "%{http_code}" ${BASE_URL}/jaeger/
# VictoriaLogs via proxy
curl -sf -o /dev/null -w "%{http_code}" ${BASE_URL}/select/vmui/
```
Expect: HTTP 200 or 301/302 for each.

---

### Suite 3: User Acceptance Tests

Validate that the user-facing workflows work end-to-end.

**3.1 One-shot prompt (localhost only)**
```bash
echo "What is 2+2?" | timeout 30 bin/ycode --no-otel --print 2>/dev/null
```
Expect: output containing "4". If no API key is configured, skip this test and note it as skipped.

**3.2 Serve subcommands**
```bash
bin/ycode serve status --port ${PORT}
bin/ycode serve audit --last 5
```
Expect: both exit 0.

**3.3 Doctor check**
```bash
bin/ycode doctor
```
Expect: exit 0, "All checks passed" or identified issues.

---

### Suite 4: Performance Tests

Baseline measurements. These are informational — no pass/fail thresholds on first run. On subsequent runs, compare against previous baselines if available.

**4.1 Health endpoint latency (p50/p99)**
```bash
# 50 requests to healthz
for i in $(seq 1 50); do
  curl -sf -o /dev/null -w "%{time_total}\n" ${BASE_URL}/healthz
done | sort -n | awk '
  {a[NR]=$1; s+=$1}
  END {
    printf "  requests: %d\n", NR
    printf "  mean:     %.3fs\n", s/NR
    printf "  p50:      %.3fs\n", a[int(NR*0.5)]
    printf "  p95:      %.3fs\n", a[int(NR*0.95)]
    printf "  p99:      %.3fs\n", a[int(NR*0.99)]
  }
'
```

**4.2 Trace ingestion throughput**
```bash
# Send 100 trace batches and measure total time
START=$(date +%s%N)
for i in $(seq 1 100); do
  curl -sf http://${HOST}:4318/v1/traces -X POST \
    -H "Content-Type: application/json" \
    -d '{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"perf-test"}}]},"scopeSpans":[{"spans":[{"traceId":"00000000000000000000000000000002","spanId":"000000000000'$(printf '%04x' $i)'","name":"perf-span-'$i'","kind":1,"startTimeUnixNano":"'$(date +%s)000000000'","endTimeUnixNano":"'$(date +%s)000000000'"}]}]}]}' \
    -o /dev/null &
done
wait
END=$(date +%s%N)
ELAPSED=$(( (END - START) / 1000000 ))
echo "  100 trace batches in ${ELAPSED}ms ($(( 100000 / (ELAPSED + 1) )) req/s)"
```

**4.3 Binary startup time**
```bash
# Measure time to run version command (cold start)
time bin/ycode version 2>&1
```

---

### Report

After all suites complete, print a summary table:

```
=== Validation Report ===
Target: ${BASE_URL}

Suite              Passed  Failed  Skipped
─────────────────  ──────  ──────  ───────
Smoke Tests           4       0        0
Integration Tests     6       0        0
Acceptance Tests      3       0        0
Performance Tests     3       0        0
─────────────────  ──────  ──────  ───────
Total                16       0        0

Performance Baselines:
  healthz p50: 0.003s  p99: 0.012s
  trace ingestion: 1250 req/s
  binary startup: 0.045s
```

For any failed tests, include the failure details below the summary.

If ALL tests pass, print: `Validation PASSED`
If ANY test fails, print: `Validation FAILED — see failures above`

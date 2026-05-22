# ycode Observability

ycode runs a complete OpenTelemetry stack — collector, Prometheus, Jaeger,
VictoriaLogs, Alertmanager, Perses — entirely in-process, as a single
binary. It serves two equally first-class purposes:

1. **Internal self-monitoring.** ycode introspects its own behavior — sessions,
   turns, LLM calls, tool calls, errors, latencies — for diagnosis,
   self-healing, and learning. The programmatic counterpart is
   `pkg/olly/query`, used by the autonomous loop and other components to
   ask "what happened during the last user query?".
2. **Third-party OTLP hub.** Any tool that emits standard OTLP/gRPC or
   OTLP/HTTP can publish to ycode without modifications on either side.
   ycode preserves the publisher's identity (`service.name`, etc.)
   end-to-end so signals from different sources can be stored, queried,
   and visualized separately.

This document covers the wire surface, the port map, the source-differentiation
contract, and the programmatic query API.

## Wire surface for OTLP publishers

ycode pins its OTLP receivers to the standard well-known ports so any
out-of-the-box OTLP client connects without configuration:

| Endpoint              | Default port | Override                                    |
| --------------------- | ------------ | ------------------------------------------- |
| OTLP/gRPC             | `4317`       | `observability.otlpGRPCPort` (config.json)  |
| OTLP/HTTP (protobuf)  | `4318`       | `observability.otlpHTTPPort`                |
| Reverse-proxy landing | `31415`      | `observability.proxyPort` / `--port`        |

> **Migration note (port change):** Earlier builds defaulted the reverse
> proxy to `58080`, which sits inside the OS ephemeral port range on both
> macOS (49152–65535) and Linux (default `ip_local_port_range` 32768–60999)
> and could race-lose to an OS-assigned ephemeral socket. The default
> moved to `31415` (below both ephemeral pools, IANA-unassigned). User
> scripts or third-party configs that hardcoded `58080` must be updated,
> or restore the old port explicitly with `ycode serve --port 58080`.

If a default port is already in use, `ycode serve` fails loud rather than
silently picking another one — third-party publishers can rely on
`localhost:4317` / `localhost:4318` working or not booting at all. To
opt back into ephemeral allocation (for tests or multi-instance setups),
set the override to a negative value.

Internal ports — Prometheus exporter (collector → Prometheus scrape),
VictoriaLogs HTTP, Jaeger query UI, Perses, Alertmanager, etc. — are
allocated from an ephemeral range and surfaced at the proxy on
`31415`. They are not part of the public wire surface.

## Source differentiation

ycode does not authenticate publishers; it differentiates them by the
standard OTel resource attributes carried on every OTLP signal.
Conventionally:

- `service.name`  — required, identifies the publisher (`ycode`,
  `external-tool`, etc.)
- `service.namespace` — optional grouping for fleets
- `service.instance.id` — distinct instance within a service

These attributes survive end-to-end:

- **Metrics:** the embedded collector's Prometheus exporter is configured
  with `resource_to_telemetry_conversion: enabled: true`, so resource
  attributes become Prometheus labels (`service_name`, `service_namespace`,
  `service_instance_id`). Filter them with PromQL like
  `ycode_turn_total{service_name="external-tool"}`.
- **Traces:** Jaeger uses `service.name` natively as the trace's service.
  Filter in the Jaeger UI by service or via the typed query API.
- **Logs:** VictoriaLogs receives logs over OTLP/HTTP and indexes resource
  attributes as fields. Use LogsQL like `{service.name="external-tool"}`.

The integration test
`internal/observability/source_attribution_test.go::TestSourceAttribution_PrometheusLabels`
publishes a synthetic metric tagged `service.name=external-tool` via OTLP
gRPC and asserts the matching label appears on the Prometheus exporter.

## Programmatic query API: `pkg/olly/query`

For ycode-internal consumers that need to introspect the stack
programmatically, `pkg/olly/query` exposes a typed Go API over the
embedded backends:

```go
import "github.com/qiangli/ycode/pkg/olly/query"

q := query.New(query.Backends{
    Metrics: query.NewPromAdapter(promDB, promEngine),
    Traces:  query.NewJaegerAdapter("http://127.0.0.1:31415/traces", nil),
    Logs:    query.NewVLAdapter("http://127.0.0.1:31415/logs", nil),
})

// Reconstruct a session timeline.
traces, _ := q.Traces(ctx, query.TraceFilter{SessionID: "sess-123"})

// Find recent errors for a turn.
logs, _ := q.Logs(ctx, query.LogFilter{TraceID: trc, Level: "ERROR"})

// Count tool calls per minute over the last hour.
res, _ := q.QueryPromQLRange(ctx,
    `rate(ycode_tool_call_total[1m])`,
    time.Now().Add(-1*time.Hour), time.Now(), 60*time.Second)
```

The PromQL adapter calls the in-process TSDB+engine directly (no HTTP
round-trip). The Jaeger and VictoriaLogs adapters use loopback HTTP
because both backends own their own query servers and offer no
in-process Go API.

`TraceFilter` / `LogFilter` use OTel semantic-convention attribute names
(`service.name`, `session.id`, `trace.id`) so the same filter shape
works across signals.

## Self-healing alert path

Alerts have two consumers in ycode: humans (via Alertmanager's standard
notification routing) and ycode itself (via the event bus, for
programmatic self-healing).

Every alert delivered through `AlertmanagerComponent.AddAlert` is
mirrored onto the configured `bus.Bus` as `bus.EventAlertFired` carrying
a `bus.AlertFiredPayload`:

```go
ch, unsub := bus.Subscribe(bus.EventAlertFired)
defer unsub()
for ev := range ch {
    var p bus.AlertFiredPayload
    _ = json.Unmarshal(ev.Data, &p)
    // p.Name, p.Severity, p.Labels, p.StartsAt, ...
    // — react: extend timeout, retry, evolve a skill, etc.
}
```

Wire the bus into the component with `comp.SetBus(b)` before serving;
without a bus, `AddAlert` is unchanged.

## Producer-side bootstrap

The producer side lives in `internal/telemetry/otel/`. A single
`NewProvider` builds the SDK with up to three signal providers (traces,
metrics, logs) and a dual exporter graph: gRPC to the running collector
+ rotating JSONL files on disk. File-mode persistence covers all three
signal types — without it, structured logs are dropped whenever the
collector is unreachable.

Configuration is on `config.ObservabilityConfig`:

| Field            | Default       | Purpose                                      |
| ---------------- | ------------- | -------------------------------------------- |
| `Enabled`        | `false`       | Only true when running `ycode serve`         |
| `CollectorAddr`  | `127.0.0.1:4317` | gRPC endpoint the SDK exports to         |
| `OTLPGRPCPort`   | `4317`        | Embedded collector OTLP/gRPC bind            |
| `OTLPHTTPPort`   | `4318`        | Embedded collector OTLP/HTTP bind            |
| `ProxyPort`      | `31415`       | Reverse-proxy landing for all UIs            |
| `PersistTraces`  | `true`        | Rotating JSONL traces                        |
| `PersistMetrics` | `true`        | Rotating JSONL metrics                       |
| `PersistLogs`    | `true`        | Rotating JSONL logs                          |
| `SampleRate`     | `1.0`         | Trace sampling (0.0–1.0)                     |

## Coverage map

ycode emits the following metrics today; the per-row file path points
at the producer site (each cross-cutting helper lives in
`internal/telemetry/otel/`).

| Metric (Prometheus form)             | Producer                                               |
| ------------------------------------ | ------------------------------------------------------ |
| `ycode_llm_call_total`               | `internal/runtime/conversation/otel.go`                |
| `ycode_llm_call_duration`            | `internal/runtime/conversation/otel.go`                |
| `ycode_llm_tokens_input/output`      | `internal/runtime/conversation/otel.go`                |
| `ycode_llm_tokens_cache_read/write`  | `internal/runtime/conversation/otel.go`                |
| `ycode_llm_cost_dollars`             | `internal/runtime/conversation/otel.go`                |
| `ycode_llm_context_window_used`      | `internal/api/otel.go`                                 |
| `ycode_tool_call_total`              | `internal/telemetry/otel/middleware.go`                |
| `ycode_tool_call_duration`           | `internal/telemetry/otel/middleware.go`                |
| `ycode_turn_duration`                | `internal/runtime/conversation/otel.go`                |
| `ycode_turn_tool_count`              | `internal/runtime/conversation/otel.go`                |
| `ycode_session_turns`                | `internal/runtime/conversation/otel.go`                |
| `ycode_compaction_total`             | `internal/runtime/conversation/otel.go`                |
| `ycode_compaction_tokens_saved`      | `internal/runtime/conversation/otel.go`                |
| `ycode_runtime_panic_total`          | `internal/telemetry/otel/panic.go` (lazy global)       |
| `ycode_bash_exec_total/duration`     | `internal/runtime/bash/exec.go` via `bash_metrics.go`  |
| `ycode_fileops_ops_total`            | `internal/tools/file.go` via `fileops_metrics.go`      |
| `ycode_fileops_bytes_total`          | `internal/tools/file.go` via `fileops_metrics.go`      |
| `ycode_http_request_total/duration`  | `internal/api/retry.go` via `http_metrics.go`          |
| `ycode_api_retry_attempts/delay`     | `internal/api/retry.go` via `http_metrics.go`          |
| `ycode_web_fetch_total/duration`     | `internal/tools/web.go` via `http_metrics.go`          |
| `ycode_web_search_total/duration/results` | `internal/tools/web.go` via `http_metrics.go`     |
| `ycode_api_error_total`              | `internal/runtime/conversation/otel.go`                |
| `ycode_message_structure_warnings`   | `internal/runtime/conversation/otel.go`                |
| `ycode_error_total`                  | `internal/runtime/conversation/otel.go`                |
| `ycode_inference_*`                  | `internal/inference/`                                  |
| `ycode_search_*`                     | `internal/tools/search.go`, `symbol_search.go`         |
| `ycode_bus_events_published/dropped` | `internal/bus/`                                        |
| `system_*` (host CPU/mem/disk/net)   | embedded collector hostmetrics receiver                |

LLM metrics carry `llm.model` and `llm.provider` attributes.
Tool metrics carry `tool_name` and `tool_success`. HTTP metrics carry
`http.method`, `http.host`, `http.status_code`, `success`. Bash metrics
carry `success`, `background`, `timed_out`, `exit_code`. File-ops metrics
carry `op` (`read`/`write`/`edit`) and `success`. Retry metrics carry
`reason` (`rate_limited`/`5xx`/`4xx`/`net_error`/etc.).

## Trace coverage

User-query → final-answer is traced end-to-end. Key spans:

| Span name              | Producer                                                          |
| ---------------------- | ----------------------------------------------------------------- |
| `ycode.session`        | `internal/runtime/conversation/runtime.go`                        |
| `ycode.turn`           | `internal/runtime/conversation/otel.go` (InstrumentedTurn)        |
| `prompt.build`         | `internal/runtime/conversation/runtime.go`                        |
| `memory.prefetch`      | `internal/runtime/session/memory_prefetch.go`                     |
| `ycode.tool.call`      | `internal/telemetry/otel/middleware.go` (per-tool, with input/output events) |
| `ycode.subagent`       | `internal/runtime/conversation/spawner.go`                        |

Tool spans capture full input + output as span events (subject to size
truncation) for self-healing replay. Recovered panics from the safety
net (`yotel.RecordPanic`) attach to the active span as a `panic` event
plus the typed error.

## Alert rules

Ten alert rules are loaded from `configs/prometheus/alerts/ycode.yml`
on startup:

| Rule                            | Trips on                                                  |
| ------------------------------- | --------------------------------------------------------- |
| `YcodeAPIErrorRate`             | LLM error rate > 10% for 5m                               |
| `YcodeAPILatencyP99`            | LLM p99 latency > 30s for 5m                              |
| `YcodeToolFailureRate`          | Tool failure rate > 25% for 5m                            |
| `YcodeTokenBudgetExhausted`     | Context window > 90% full                                 |
| `YcodeCostThreshold`            | Projected daily cost > $10                                |
| `YcodeCompactionFrequency`      | More than 3 compactions in the last hour                  |
| `YcodeRuntimePanic`             | Any recovered panic in the last 5m                        |
| `YcodeExcessiveToolCallsPerTurn`| p95 tool count per turn > 50 for 10m                      |
| `YcodeToolLatencyTimeoutClass`  | Tool p99 latency > 60s for 5m                             |
| `YcodeTokenInputRateSpike`      | Input token rate > 1.5x previous hour for 10m             |
| `YcodeHTTPRetryStorm`           | API retry rate > 1/s for 5m                               |
| `YcodeBackendDown`              | Any `up{}` = 0 (Prometheus / Jaeger / VL / Collector) for 1m |

## File layout

| Area                         | Path                                  |
| ---------------------------- | ------------------------------------- |
| Producer (SDK bootstrap)     | `internal/telemetry/otel/`            |
| Consumer orchestrator        | `internal/observability/`             |
| Embedded collector lifecycle | `internal/observability/collector.go` |
| Backend libraries            | `pkg/otel/`                           |
| Programmatic query API       | `pkg/olly/query/`                     |
| Alert bus events             | `internal/bus/events_alert.go`        |
| Cross-cutting metric helpers | `internal/telemetry/otel/{panic,bash_metrics,fileops_metrics,http_metrics}.go` |
| Alert rules                  | `configs/prometheus/alerts/ycode.yml` |

## See also

- [docs/architecture.md](./architecture.md) — full system architecture
- [docs/autonomous-loop.md](./autonomous-loop.md) — how self-healing
  consumes alert and query data

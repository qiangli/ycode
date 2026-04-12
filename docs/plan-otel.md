# Plan: Embedded OTEL Observability Stack

## Context

ycode integrates a full observability stack that runs entirely in-process as goroutines. The single binary contains:

- **OTEL SDK** with LLM-specific metrics, traces, and structured logs
- **OTEL Collector** (embedded) routing signals to 3 backends
- **Prometheus** (embedded TSDB + PromQL) for metrics
- **VictoriaLogs** (git submodule) for logs
- **Jaeger** (git submodule) for traces
- **Perses** (git submodule) for dashboards
- **Alertmanager** (embedded) for alert routing

All behind a single reverse proxy on a configurable port (default `:58080`).

## Architecture

```
ycode OTEL SDK (per-instance ID)
       │ gRPC OTLP
       ▼
  OTEL Collector (in-process, go.opentelemetry.io/collector/otelcol)
       │
  ┌────┼──────────────┐
  ▼    ▼              ▼
Prometheus    VictoriaLogs    Jaeger
(metrics)     (logs)          (traces)
  │
Alertmanager    Perses
(alerts)        (dashboards, queries Prometheus)
```

### Signal Routing

| Signal | Collector Pipeline | Exporter | Backend |
|--------|-------------------|----------|---------|
| Metrics | `metrics` | `prometheus` (`:8889`) | Embedded Prometheus TSDB |
| Logs | `logs` | `otlphttp/vlogs` (`:9428`) | Embedded VictoriaLogs |
| Traces | `traces` | `otlp/jaeger` (`:14317`) | Embedded Jaeger |

## Component Embedding Strategy

| Component | Strategy | Source |
|-----------|----------|--------|
| OTEL Collector | Go module import | `go.opentelemetry.io/collector/otelcol` |
| Prometheus | Go module import | `github.com/prometheus/prometheus/{tsdb,promql}` |
| Alertmanager | Go module import | `github.com/prometheus/alertmanager/{dispatch,api}` |
| VictoriaLogs | Git submodule + adapter | `external/victorialogs/` |
| Jaeger | Git submodule + adapter | `external/jaeger/` |
| Perses | Git submodule + adapter | `external/perses/` |

### Submodule Adapter Pattern

For components not designed as embeddable libraries (VictoriaLogs, Jaeger, Perses):

1. Add as git submodule: `git submodule add <repo> external/<name>`
2. Create adapter package: `external/<name>/embed.go`
   - Exposes `Start(ctx, port, dataDir)` and `Stop(ctx)`
   - Replaces flag/CLI parsing with programmatic config
   - Replaces signal handling with context cancellation
3. Create Component wrapper: `internal/observability/<name>.go`
   - Implements `Component` interface
   - Calls adapter's `Start()` in a goroutine

## Stack Startup Order

Components start in dependency order (each `Start()` is non-blocking):

1. **VictoriaLogs** — log sink must be ready
2. **Jaeger** — trace sink must be ready
3. **OTEL Collector** — routes signals to VictoriaLogs + Jaeger + Prometheus
4. **Prometheus** — scrapes collector's `/metrics` endpoint
5. **Alertmanager** — receives alerts from Prometheus rule evaluation
6. **Perses** — queries Prometheus API for dashboard panels

Shutdown is reverse order.

## CLI: `ycode serve`

| Command | Description |
|---------|-------------|
| `ycode serve --port 58080` | Foreground server |
| `ycode serve --detach` | Background daemon |
| `ycode serve stop` | Stop running server |
| `ycode serve status` | Component health table |
| `ycode serve dashboard` | Open Perses in browser |
| `ycode serve reset` | Wipe observability data |
| `ycode serve audit` | Show conversation records |

### Auto-Start

`ycode --port 58080` (without `serve`) auto-starts the server if none is running. On exit, prompts to keep running only if this instance started it.

`ycode --no-otel` skips all telemetry.

## Instance Tracking

Each ycode process attaches `service.instance.id` (UUID) to all OTEL resource attributes. Enables per-client filtering across all backends.

## Key Files

| File | Purpose |
|------|---------|
| `internal/telemetry/otel/provider.go` | OTEL SDK (TracerProvider + MeterProvider + InstanceID) |
| `internal/telemetry/otel/instruments.go` | 15 metric instruments |
| `internal/telemetry/otel/middleware.go` | Tool call tracing with full I/O capture |
| `internal/telemetry/otel/request_logger.go` | Conversation record JSONL logger |
| `internal/collector/embedded.go` | In-process OTEL Collector |
| `internal/collector/config.go` | Collector YAML with 3-way pipeline routing |
| `internal/observability/component.go` | `Component` interface |
| `internal/observability/stack.go` | `StackManager` lifecycle orchestration |
| `internal/observability/proxy.go` | Reverse proxy (single port entry point) |
| `internal/observability/prometheus.go` | Embedded Prometheus TSDB + PromQL |
| `internal/observability/alertmanager.go` | Embedded alert dispatcher |
| `internal/observability/victorialogs.go` | VictoriaLogs Component wrapper |
| `internal/observability/jaeger.go` | Jaeger Component wrapper |
| `internal/observability/perses.go` | Perses Component wrapper |
| `internal/observability/dashboards/` | Default dashboard JSON configs |
| `external/victorialogs/` | Git submodule + embed.go adapter |
| `external/jaeger/` | Git submodule + embed.go adapter |
| `external/perses/` | Git submodule + embed.go adapter |
| `cmd/ycode/serve.go` | `ycode serve` command + `buildStackManager()` |
| `cmd/ycode/autoserve.go` | Auto-start logic + exit prompt |
| `cmd/ycode/otel.go` | `setupOTEL()` SDK initialization |

## Metrics

15 instruments: 8 LLM (call duration/count, tokens in/out/cache, cost, context usage), 2 tool (duration/count), 3 turn (duration, tool count, session turns), 2 compaction (count, tokens saved).

## Dependencies

All Apache-2.0, MIT, or BSD licensed. No AGPL (Grafana excluded).

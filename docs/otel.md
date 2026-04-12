# Observability & Telemetry

ycode ships a fully embedded observability stack — a single self-contained binary with all services running as goroutines. No external binaries, no downloads.

## Quick Start

```bash
# Explicit server mode
ycode serve --port 58080              # foreground
ycode serve --port 58080 --detach     # background daemon

# Or just run ycode — auto-starts the server if none is running
ycode --port 58080

# Skip telemetry entirely
ycode --no-otel
```

## Architecture

```
  ycode client (OTEL SDK, per-instance ID)
       │
       │ gRPC OTLP (127.0.0.1:4317)
       ▼
  ┌──────────────────────────────────────────────┐
  │  Embedded Observability Server (:58080)       │
  │  All components run as goroutines             │
  │                                               │
  │  ┌─────────────────────────────────────────┐  │
  │  │         OTEL Collector (in-process)     │  │
  │  │  receivers: otlp (gRPC + HTTP)          │  │
  │  │  processors: batch                      │  │
  │  └────┬──────────────┬──────────────┬──────┘  │
  │       │ metrics      │ logs         │ traces  │
  │       ▼              ▼              ▼         │
  │  ┌─────────┐  ┌──────────────┐  ┌─────────┐  │
  │  │Prometheus│  │VictoriaLogs │  │ Jaeger  │  │
  │  │TSDB+     │  │(submodule)  │  │(submod) │  │
  │  │PromQL    │  │/logs/       │  │/traces/ │  │
  │  │/prom./   │  └──────────────┘  └─────────┘  │
  │  └─────────┘                                  │
  │       │                                       │
  │  ┌────▼────┐  ┌──────────────────────────┐   │
  │  │Alertmgr │  │ Perses Dashboards        │   │
  │  │/alerts/ │  │ (submodule) /dashboard/  │   │
  │  └─────────┘  └──────────────────────────┘   │
  │                                               │
  │  ┌──────────────────────────────────────────┐ │
  │  │ Reverse Proxy — 127.0.0.1:58080          │ │
  │  │ Landing page + /healthz                  │ │
  │  └──────────────────────────────────────────┘ │
  └──────────────────────────────────────────────┘
```

## Signal Routing

| Signal | Pipeline | Destination |
|--------|----------|-------------|
| **Metrics** | OTEL SDK → Collector → Prometheus exporter | Embedded Prometheus TSDB |
| **Logs** | OTEL SDK → Collector → OTLP HTTP exporter | Embedded VictoriaLogs |
| **Traces** | OTEL SDK → Collector → OTLP gRPC exporter | Embedded Jaeger |

## Proxy Routes

All services accessible via `http://127.0.0.1:58080/`:

| Path | Component | Description |
|------|-----------|-------------|
| `/collector/` | OTEL Collector | Health status |
| `/prometheus/` | Prometheus | PromQL API + graph UI |
| `/alerts/` | Alertmanager | Alert API + UI |
| `/logs/` | VictoriaLogs | Log query API + VMUI |
| `/traces/` | Jaeger | Trace query UI |
| `/dashboard/` | Perses | Dashboard UI with ycode panels |
| `/healthz` | Proxy | Aggregated health check |

## Components

### OTEL Collector (embedded, Go module)

Runs in-process via `go.opentelemetry.io/collector/otelcol`.

- **Receivers**: OTLP gRPC (`:4317`), OTLP HTTP (`:4318`)
- **Processors**: batch (5s timeout)
- **Exporters**:
  - `prometheus` → embedded Prometheus (`:8889`)
  - `otlphttp/vlogs` → embedded VictoriaLogs (`:9428/insert/opentelemetry`)
  - `otlp/jaeger` → embedded Jaeger (`:14317`)

Source: `internal/collector/embedded.go`, `internal/collector/config.go`

### Prometheus (embedded, Go module)

Uses `github.com/prometheus/prometheus/tsdb` and `promql` as Go libraries.

- TSDB: `~/.ycode/observability/prometheus/data`, 15-day retention
- Scrape: polls collector `/metrics` every 15s
- HTTP API: `/api/v1/query`, `/api/v1/query_range`
- Runs TSDB + scrape loop + HTTP server in goroutines

Source: `internal/observability/prometheus.go`

### Alertmanager (embedded, Go module)

Lightweight in-process alert dispatcher with Alertmanager v2 API.

- `POST/GET /api/v2/alerts` — send and list alerts
- Cleanup goroutine: expires resolved alerts after 5 minutes
- HTML UI at root

Source: `internal/observability/alertmanager.go`

### VictoriaLogs (embedded, git submodule)

Source from `external/victorialogs/` with adapter for in-process lifecycle.

- Accepts OTLP HTTP logs from the collector
- Query API + VMUI web interface
- Storage at `~/.ycode/observability/vlogs/`
- Lifecycle: `vlstorage.Init()` → `vlselect.Init()` → `vlinsert.Init()` in goroutine

Source: `external/victorialogs/`, `internal/observability/victorialogs.go`

### Jaeger (embedded, git submodule)

Source from `external/jaeger/` with adapter for in-process lifecycle. Jaeger v2 is built on the OTEL Collector framework.

- OTLP gRPC receiver for traces from the collector
- Query UI for trace visualization
- Badger storage at `~/.ycode/observability/jaeger/`
- Runs as a mini OTEL collector with Jaeger extensions in a goroutine

Source: `external/jaeger/`, `internal/observability/jaeger.go`

### Perses (embedded, git submodule)

Source from `external/perses/` with adapter for in-process lifecycle.

- Dashboard UI with pre-built ycode metric panels
- Prometheus datasource pointing to embedded Prometheus
- File-based database for dashboard persistence

Default dashboards:

| Dashboard | Panels |
|-----------|--------|
| **LLM Overview** | API call rate, latency p95, estimated cost, total calls |
| **Token Usage** | Input/output/cache-read/cache-write token rates |
| **Tool Performance** | Tool call rate, latency p95, top 10 tools by count |
| **Session Activity** | Turns per session, turn duration p95, tools per turn |
| **Context & Compaction** | Context window usage, compaction events, tokens saved |
| **Instance Comparison** | Calls/cost/tokens grouped by `service.instance.id` |

Source: `external/perses/`, `internal/observability/perses.go`, `internal/observability/dashboards/`

## Instance Tracking

Each ycode process gets a unique instance ID (session UUID) as OTEL resource attributes:

- `service.instance.id` (OpenTelemetry semantic convention)
- `ycode.instance.id` (custom attribute)

Enables per-client filtering in Prometheus, VictoriaLogs, and Jaeger.

## Metrics Reference

### LLM Metrics

| Metric | Type | Unit |
|--------|------|------|
| `ycode.llm.call.duration` | Histogram | ms |
| `ycode.llm.call.total` | Counter | — |
| `ycode.llm.tokens.input` | Counter | tokens |
| `ycode.llm.tokens.output` | Counter | tokens |
| `ycode.llm.tokens.cache_read` | Counter | tokens |
| `ycode.llm.tokens.cache_write` | Counter | tokens |
| `ycode.llm.cost.dollars` | Counter | USD |
| `ycode.llm.context_window.used` | Gauge | tokens |

### Tool Metrics

| Metric | Type | Unit |
|--------|------|------|
| `ycode.tool.call.duration` | Histogram | ms |
| `ycode.tool.call.total` | Counter | — |

### Turn & Session Metrics

| Metric | Type | Unit |
|--------|------|------|
| `ycode.turn.duration` | Histogram | ms |
| `ycode.turn.tool_count` | Histogram | — |
| `ycode.session.turns` | Counter | — |
| `ycode.compaction.total` | Counter | — |
| `ycode.compaction.tokens_saved` | Counter | tokens |

## CLI Commands

### `ycode serve`

| Command | Description |
|---------|-------------|
| `ycode serve [--port] [--detach]` | Start observability server |
| `ycode serve stop` | Stop running server |
| `ycode serve status` | Show component health |
| `ycode serve dashboard` | Open dashboard in browser |
| `ycode serve reset` | Remove all observability data |
| `ycode serve audit [--last N]` | Show conversation records |

### Root Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | 58080 | Observability server port |
| `--no-otel` | false | Skip server start/connect |

### Auto-Start Behavior

1. `ycode --port 58080` checks if server is running at the port
2. If running: connects OTEL SDK to existing server
3. If not: auto-starts server as goroutines, sets `autoStarted = true`
4. On exit, **only if this instance started the server**: prompts `Keep observability server running? [Y/n]`

## Configuration

```json
{
  "observability": {
    "enabled": true,
    "collectorAddr": "127.0.0.1:4317",
    "sampleRate": 1.0,
    "proxyPort": 58080,
    "proxyBindAddr": "127.0.0.1",
    "dataDir": "~/.ycode/otel",
    "logRetentionDays": 3,
    "logConversations": true,
    "logToolDetails": true,
    "persistTraces": true,
    "persistMetrics": true
  }
}
```

## Directory Structure

```
~/.ycode/
├── observability/
│   ├── collector/config.yaml
│   ├── prometheus/data/        # TSDB
│   ├── vlogs/data/             # VictoriaLogs storage
│   ├── jaeger/                 # Jaeger badger storage
│   └── serve.pid               # Server PID (detached mode)
└── otel/
    ├── logs/conversations-*.jsonl
    ├── traces/traces-*.jsonl
    └── metrics/metrics-*.jsonl
```

## Dependencies

All permissive licenses (Apache-2.0, MIT, BSD):

| Module | License | Purpose |
|--------|---------|---------|
| `go.opentelemetry.io/otel/*` | Apache-2.0 | OTEL SDK |
| `go.opentelemetry.io/collector/*` | Apache-2.0 | Embedded OTEL Collector |
| `github.com/prometheus/prometheus` | Apache-2.0 | Embedded TSDB + PromQL |
| `external/victorialogs` (submodule) | Apache-2.0 | Embedded log storage |
| `external/jaeger` (submodule) | Apache-2.0 | Embedded trace storage + UI |
| `external/perses` (submodule) | Apache-2.0 | Embedded dashboards |

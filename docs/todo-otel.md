# OTEL Observability Stack — Implementation Checklist

All components run in-process as goroutines. Single binary, zero external downloads.

## Phase 1: OTEL SDK Instrumentation

### 1.1 Package: `internal/telemetry/otel/`
- [x] `provider.go` — TracerProvider + MeterProvider with dual export (gRPC + file)
- [x] `attributes.go` — Semantic attribute keys for LLM, tool, session, turn, compaction, instance
- [x] `instruments.go` — 15 pre-created OTEL metric instruments
- [x] `cost.go` — Model pricing table with longest-prefix matching
- [x] `bridge.go` — `OTELSink` implementing existing `telemetry.Sink` interface
- [x] `middleware.go` — Tool middleware with full input/output as span events
- [x] `request_logger.go` — Rotating daily JSONL for conversation records
- [x] `retention.go` — Background cleanup of expired date-stamped files
- [x] `shutdown.go` — Graceful shutdown for all providers

### 1.2 API instrumentation
- [x] `internal/api/otel.go` — `InstrumentedProvider` wrapping `Send()` with all `llm.*` attributes
- [x] Works for both Anthropic and OpenAI providers

### 1.3 Conversation runtime instrumentation
- [x] `internal/runtime/conversation/otel.go` — `InstrumentedTurn()` span with turn index, tool calls
- [x] `InstrumentedTurnWithRecovery()` — span with compaction/pruning events
- [x] `OTELConfig` struct, `SetOTEL()`, `LogConversation()`, `recordTurnMetrics()`

### 1.4 slog bridge
- [x] `teeLogHandler` in `cmd/ycode/otel.go` — forwards to both stderr and OTEL LoggerProvider

### 1.5 Instance tracking
- [x] `InstanceID` field in `ProviderConfig` (UUID per ycode process)
- [x] `service.instance.id` + `ycode.instance.id` OTEL resource attributes
- [x] `AttrInstanceID` in `attributes.go`

## Phase 2: Local Disk Persistence

- [x] Directory layout: `~/.ycode/otel/{logs,traces,metrics}/`
- [x] `ConversationRecord` and `ToolCallLog` types
- [x] File-based OTLP exporters (stdout trace/metric to rotating JSONL)
- [x] Config: DataDir, LogRetentionDays, LogToolDetails, PersistTraces, PersistMetrics
- [x] Retention cleanup background goroutine

## Phase 3: Embedded OTEL Collector

- [x] `internal/collector/embedded.go` — In-process collector via `otelcol.NewCollector()`
- [x] `internal/collector/config.go` — Generate collector YAML with pipeline routing
- [x] Collector pipeline: metrics → Prometheus exporter
- [x] Collector pipeline: logs → otlphttp exporter → VictoriaLogs
- [x] Collector pipeline: traces → otlp exporter → Jaeger
- [x] Add `otlp` exporter factory to embedded collector (for Jaeger)
- [x] Add `otlphttp` exporter factory to embedded collector (for VictoriaLogs)

## Phase 4: Component Interface + Stack Manager

- [x] `internal/observability/component.go` — `Component` interface (Name, Start, Stop, Healthy, HTTPHandler)
- [x] `internal/observability/stack.go` — `StackManager` with component lifecycle
- [x] `internal/observability/proxy.go` — Reverse proxy with AddRoute + AddHandler
- [x] `internal/observability/types.go` — RemoteWriteTarget, FederationTarget types
- [x] `internal/observability/ports.go` — Port allocator

## Phase 5: Embedded Prometheus (Go module import, goroutine)

- [x] `internal/observability/prometheus.go` — Embedded TSDB + PromQL engine
- [x] Open `tsdb.DB` at `~/.ycode/observability/prometheus/data`
- [x] PromQL engine for `/api/v1/query` and `/api/v1/query_range`
- [x] Scrape loop: poll collector's `/metrics` endpoint every 15s
- [x] HTTP handler mounted at `/prometheus/` via proxy
- [x] 15-day retention, configurable
- [x] Implements `Component` interface (Start in goroutine, non-blocking)

## Phase 6: Embedded Alertmanager (Go module import, goroutine)

- [x] `internal/observability/alertmanager.go` — Embedded alert dispatcher
- [x] Alert API: POST/GET `/api/v2/alerts` (Alertmanager v2 compatible)
- [x] Cleanup goroutine: expire resolved alerts after 5 minutes
- [x] HTML UI at root showing current alerts
- [x] HTTP handler mounted at `/alerts/` via proxy
- [x] Implements `Component` interface

## Phase 7: Embedded VictoriaLogs (git submodule, goroutine)

### 7.1 Submodule setup
- [x] `git submodule add https://github.com/VictoriaMetrics/VictoriaLogs external/victorialogs`
- [x] Pin to stable release tag (v1.49.0)

### 7.2 Adapter package
- [x] `internal/observability/victorialogs.go` — Direct import of VictoriaLogs packages
- [x] Adapt `vlstorage.Init()` → `vlselect.Init()` → `vlinsert.Init()` lifecycle
- [x] Replace flag-based config with programmatic `flag.Set()`
- [x] Replace signal handling with context cancellation
- [x] `go.mod` replace directive: `github.com/VictoriaMetrics/VictoriaLogs => ./external/victorialogs`

### 7.3 Component integration
- [x] `VictoriaLogsComponent` implementing Component interface
- [x] Start in goroutine via `httpserver.Serve()`
- [x] Context cancellation triggers `httpserver.Stop()` → `vlinsert.Stop()` → `vlselect.Stop()` → `vlstorage.Stop()`
- [x] Reverse proxy at `/logs/` via stack manager

### 7.4 Wiring
- [x] Collector routes logs pipeline → VictoriaLogs OTLP HTTP endpoint (`:9428/insert/opentelemetry`)
- [x] `go build ./...` compiles with VictoriaLogs submodule

## Phase 8: Embedded Jaeger (git submodule, goroutine)

### 8.1 Submodule setup
- [x] `git submodule add https://github.com/jaegertracing/jaeger external/jaeger`
- [x] Pin to stable v2.x release tag (v2.17.0)

### 8.2 Adapter package
- [x] `external/jaeger/cmd/jaeger/embed/embed.go` — Re-exports `Components` factory from `cmd/jaeger/internal`
- [x] Build Jaeger all-in-one using `otelcol.NewCollector()` with Jaeger extensions
- [x] Configure: jaeger_storage (memory, 100K traces), jaeger_query (UI), OTLP receiver
- [x] Programmatic YAML config generated by `JaegerComponent.generateConfig()`
- [x] `go.mod` replace directive: `github.com/jaegertracing/jaeger => ./external/jaeger`

### 8.3 Component integration
- [x] `JaegerComponent` implementing Component interface
- [x] Start in goroutine via `otelcol.Collector.Run()`
- [x] OTLP gRPC on `:14317`, Query UI on `:16686`
- [x] Query UI reverse-proxied at `/traces/` via stack manager

### 8.4 Wiring
- [x] Collector routes traces pipeline → Jaeger OTLP gRPC endpoint (`:14317`)
- [x] `go build ./...` compiles with Jaeger submodule

## Phase 9: Embedded Perses (git submodule, goroutine)

### 9.1 Submodule setup
- [x] `git submodule add https://github.com/perses/perses external/perses`
- [x] Pin to stable release tag (v0.53.1)

### 9.2 Adapter package
- [x] `external/perses/embed/embed.go` — Wraps `core.New(conf)` → `runner.Start()` factory
- [x] Configure: file-based database at `~/.ycode/observability/perses/data`
- [x] Programmatic config (no CLI/flags)
- [x] `external/perses/ui/embed_stub.go` — Stub for React UI embed.FS
- [x] `go.mod` replace directive: `github.com/perses/perses => ./external/perses`

### 9.3 Default dashboards
- [x] `internal/observability/dashboards/default_project.json` — ycode dashboard project
- [x] LLM Overview: call rate, latency p95, cost, total calls
- [x] Token Usage: input/output/cache rates by instance
- [x] Tool Performance: call rate, latency, top 10 tools
- [x] Session Activity: turns, duration, tools per turn
- [x] Context & Compaction: window usage, compaction events, tokens saved
- [x] Instance Comparison: metrics grouped by `service.instance.id`
- [ ] Auto-provision dashboards on first Perses start

### 9.4 Component integration
- [x] `PersesComponent` implementing Component interface
- [x] Start in goroutine via `persesembed.Start()`
- [x] Reverse proxy at `/dashboard/` via stack manager

### 9.5 Wiring
- [x] Perses configured with file-based database
- [x] `go build ./...` compiles with Perses submodule

## Phase 10: Stack Assembly + CLI

### 10.1 Collector pipeline config
- [x] `internal/collector/config.go` — GenerateYAML with 3-way routing
  - metrics pipeline → prometheus exporter
  - logs pipeline → otlphttp/vlogs exporter → VictoriaLogs
  - traces pipeline → otlp/jaeger exporter → Jaeger
- [x] `internal/collector/embedded.go` — otlp + otlphttp + prometheus exporter factories

### 10.2 Stack manager
- [x] `cmd/ycode/serve.go` `buildStackManager()` wires all 6 components in order:
  1. VictoriaLogs (log sink, :9428)
  2. Jaeger (trace sink, OTLP :14317, Query :16686)
  3. OTEL Collector (gRPC :4317, HTTP :4318, Prometheus :8889)
  4. Prometheus (embedded TSDB, scrapes :8889)
  5. Alertmanager (embedded dispatcher)
  6. Perses (dashboards, :18080)
- [x] Each component starts in its own goroutine (non-blocking)

### 10.3 Proxy routes
- [x] `/collector/` → OTEL Collector health (in-process handler)
- [x] `/prometheus/` → Prometheus PromQL API (in-process handler)
- [x] `/alerts/` → Alertmanager API + UI (in-process handler)
- [x] `/logs/` → VictoriaLogs (reverse proxy to :9428)
- [x] `/traces/` → Jaeger Query UI (reverse proxy to :16686)
- [x] `/dashboard/` → Perses (reverse proxy to :18080)

### 10.4 CLI commands
- [x] `ycode serve [--port] [--detach]` — run server
- [x] `ycode serve stop` — stop server
- [x] `ycode serve status` — component health
- [x] `ycode serve dashboard` — open browser
- [x] `ycode serve reset` — wipe data
- [x] `ycode serve audit` — conversation records
- [x] `--port` and `--no-otel` root flags
- [x] Auto-start server when no server running
- [x] Exit prompt only when this instance auto-started server

## Phase 11: Cleanup

- [x] Delete `internal/observability/logstore.go` (replaced by VictoriaLogs)
- [x] Delete `internal/observability/download.go` (no binary downloads)
- [x] Delete `internal/observability/process.go` (no subprocess management)
- [x] Remove unused imports and dead code
- [x] `go mod tidy`
- [x] `go vet ./...` clean
- [x] `go build ./...` succeeds

## Phase 12: Documentation

- [x] `docs/otel.md` — Full architecture doc with all 6 components
- [x] `docs/plan-otel.md` — Updated architecture plan
- [x] `docs/todo-otel.md` — This checklist, all items checked

## Verification

- [x] `go build ./...` succeeds with all submodules
- [x] Single binary: all components embedded, no `~/.ycode/bin/` needed
- [ ] `ycode serve --port 58080` starts all 6 components as goroutines
- [ ] `curl localhost:58080/healthz` — all components healthy
- [ ] `curl localhost:58080/prometheus/api/v1/query?query=up` — returns data
- [ ] `curl localhost:58080/logs/` — VictoriaLogs VMUI loads
- [ ] `curl localhost:58080/traces/` — Jaeger Query UI loads
- [ ] `curl localhost:58080/dashboard/` — Perses dashboards
- [ ] Run ycode session → metrics in Prometheus, logs in VictoriaLogs, traces in Jaeger
- [ ] Two ycode instances → distinguishable by `service.instance.id`
- [ ] `ycode --no-otel` — starts without telemetry
- [ ] `make build && make test` — clean

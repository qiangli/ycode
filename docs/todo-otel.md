# OTEL/OTLP Integration — Implementation Checklist

## Phase 1: OTEL SDK Instrumentation

### 1.1 Package: `internal/telemetry/otel/`
- [x] `provider.go` — TracerProvider + MeterProvider with dual export (gRPC + file)
- [x] `attributes.go` — Semantic attribute keys for LLM, tool, session, turn, compaction
- [x] `instruments.go` — 15 pre-created OTEL metric instruments
- [x] `cost.go` — Model pricing table with longest-prefix matching
- [x] `bridge.go` — `OTELSink` implementing existing `telemetry.Sink` interface
- [x] `middleware.go` — Tool middleware with full input/output as span events
- [x] `request_logger.go` — Rotating daily JSONL for conversation records
- [x] `retention.go` — Background cleanup of expired date-stamped files
- [x] `shutdown.go` — Graceful shutdown for all providers

### 1.2 API instrumentation
- [x] `internal/api/otel.go` — `InstrumentedProvider` wrapping `Send()` in `ycode.api.call` span with all `llm.*` attributes
- [x] Works for both Anthropic and OpenAI (wraps the `Provider` interface)

### 1.3 Conversation runtime instrumentation
- [x] `internal/runtime/conversation/otel.go` `InstrumentedTurn()` — Span `ycode.conversation.turn` with turn index, tool calls, tool names
- [x] `internal/runtime/conversation/otel.go` `InstrumentedTurnWithRecovery()` — Span with compaction/pruning events
- [x] `OTELConfig` struct with Tracer, Instruments, RequestLogger, Provider fields
- [x] `SetOTEL()` method on Runtime to wire instrumentation
- [x] `LogConversation()` — Logs full ConversationRecord after turn completes
- [x] `recordTurnMetrics()` — Records per-turn LLM metrics via OTEL instruments

### 1.4 RequestLogger wiring
- [x] `RequestLogger` field in `conversation.OTELConfig`
- [x] `LogConversation()` method on Runtime writes full `ConversationRecord` with tool details

### 1.5 slog bridge
- [x] `teeLogHandler` in `cmd/ycode/otel.go` — forwards to both original stderr and OTEL `otelslog.NewHandler`
- [x] `slog.SetDefault()` called during OTEL init to bridge all slog calls to OTEL logs

## Phase 2: Local Disk Persistence

- [x] Directory layout under `~/.ycode/otel/{logs,traces,metrics}/`
- [x] `ConversationRecord` and `ToolCallLog` types
- [x] File-based OTLP exporters (stdout/stdouttrace, stdout/stdoutmetric)
- [x] Config additions (DataDir, LogRetentionDays, LogToolDetails, PersistTraces, PersistMetrics)
- [x] Retention cleanup background goroutine

## Phase 3: OpenTelemetry Collector

- [x] `internal/collector/manager.go` — Subprocess lifecycle
- [x] `internal/collector/config.go` — Generate collector YAML
- [x] `internal/collector/download.go` — Binary downloader
- [x] `internal/collector/embed.go` — Embedded default config YAML via `//go:embed`
- [x] `internal/collector/default_config.yaml` — Default collector config

## Phase 4: Built-in Prometheus Stack with Reverse Proxy

### 4.1 Package: `internal/observability/`
- [x] `ports.go` — PortAllocator with persistence
- [x] `process.go` — Generic child process manager
- [x] `proxy.go` — Reverse proxy with path-based routing + landing page
- [x] `stack.go` — StackManager orchestrating all components
- [x] `prometheus.go` — Config generation
- [x] `alertmanager.go` — Config generation
- [x] `karma.go` — Config generation
- [x] `perses.go` — Config generation
- [x] `victorialogs.go` — Args generation
- [x] `download.go` — Binary downloader for all components
- [x] `gateway.go` — Remote-write and federation YAML generation

### 4.2 Config extension
- [x] `ObservabilityConfig` struct with all fields
- [x] Merge logic in `mergeFromFile()`

## Phase 5: CLI Commands

- [x] `ycode observe start`
- [x] `ycode observe stop`
- [x] `ycode observe status`
- [x] `ycode observe download`
- [x] `ycode observe dashboard`
- [x] `ycode observe alerts`
- [x] `ycode observe config`
- [x] `ycode observe reset`
- [x] `ycode observe logs [component]` — Tail component log file
- [x] `ycode observe audit [--last N]` — Show recent conversation records

## Phase 6: Config Templates and Dashboards

- [x] `configs/otelcol/builder-config.yaml` — OCB manifest
- [x] `configs/otelcol/default.yaml` — Default collector config
- [x] `configs/prometheus/alerts/ycode.yml` — 6 alert rules
- [x] `configs/prometheus/default.yml` — Default Prometheus config
- [x] `configs/alertmanager/default.yml` — Default Alertmanager config
- [x] `configs/karma/default.yaml` — Default Karma config
- [x] `configs/perses/default.yaml` — Default Perses config
- [x] `configs/proxy/landing.html` — Landing page template

## Phase 7: Startup Wiring and Tests

### Wiring
- [x] `cmd/ycode/otel.go` — `setupOTEL()` with dual-export provider, returns `otelResult` with `convOTEL`
- [x] OTEL tool middleware applied to registry
- [x] Retention cleanup goroutine started
- [x] Shutdown registered
- [x] slog bridge to OTEL LoggerProvider
- [x] Makefile `collector` target

### Tests
- [x] `cost_test.go` — Pricing lookup and cost estimation
- [x] `request_logger_test.go` — JSONL logging with/without tool details
- [x] `retention_test.go` — Old file cleanup
- [x] `ports_test.go` — Port allocation, persistence, reload, release
- [x] `proxy_test.go` — Reverse proxy with httptest backends (routing, landing page, healthz, 404)

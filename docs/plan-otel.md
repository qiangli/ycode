# Plan: Integrate OTEL/OTLP and Built-in Prometheus Stack into ycode

## Context

ycode has custom telemetry (`internal/telemetry/`) with a `Sink` interface, `SessionTracer`, and `AnalyticsCollector`, plus SQLite-backed tool metrics. None of this exports to standard observability backends. This plan adds:

1. **OTEL SDK instrumentation** with rich LLM-specific metrics exported via OTLP
2. **Full conversation + tool call logging** (prompts, responses, tool params/output) persisted locally for audit/debug/self-healing
3. **OpenTelemetry Collector** as a managed subprocess for signal routing
4. **Built-in Prometheus stack** behind a localhost reverse proxy on a fixed port

---

## Phase 1: OTEL SDK Instrumentation with Detailed LLM Metrics

### 1.1 New package: `internal/telemetry/otel/`

**Dependencies to add** (all Apache-2.0):
- `go.opentelemetry.io/otel`
- `go.opentelemetry.io/otel/sdk/trace`
- `go.opentelemetry.io/otel/sdk/metric`
- `go.opentelemetry.io/otel/sdk/log`
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc`
- `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc`
- `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc`
- `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp` (for VictoriaLogs)
- `go.opentelemetry.io/otel/bridge/otelslog`

**Files:**

| File | Purpose |
|------|---------|
| `provider.go` | `OTELProvider` holding TracerProvider, MeterProvider, LoggerProvider. Configures file-based exporters (to `~/.ycode/otel/`) alongside OTLP gRPC exporters. |
| `attributes.go` | Semantic attribute constants (see 1.2 below). |
| `bridge.go` | `OTELSink` implementing `telemetry.Sink`. Translates existing events to OTEL spans + metrics. |
| `middleware.go` | OTEL-aware tool middleware — child span per tool invocation with **full input params and output** captured as span events/attributes. |
| `request_logger.go` | Full conversation logger: captures system prompt, user messages, full response, tool call details. Writes rotating JSONL to `~/.ycode/otel/logs/`. |
| `instruments.go` | Pre-created OTEL metric instruments (histograms, counters, gauges). |
| `shutdown.go` | Graceful shutdown for all providers. |

### 1.2 Detailed metrics and span attributes

Every API call span (`ycode.api.call`) carries these attributes (sourced from `api.Request`, `api.Response`, `api.Usage`, and `usage.Tracker`):

| Attribute | Source | Type |
|-----------|--------|------|
| `llm.provider` | `provider.Kind()` (anthropic, openai, ollama, etc.) | string |
| `llm.model` | `api.Request.Model` / `api.Response.Model` | string |
| `llm.max_tokens` | `api.Request.MaxTokens` (context window budget) | int |
| `llm.temperature` | `api.Request.Temperature` | float |
| `llm.tokens.input` | `api.Usage.InputTokens + PromptTokens` | int |
| `llm.tokens.output` | `api.Usage.OutputTokens + CompletionTokens` | int |
| `llm.tokens.cache_creation` | `api.Usage.CacheCreationInput` | int |
| `llm.tokens.cache_read` | `api.Usage.CacheReadInput` | int |
| `llm.tokens.total` | input + output | int |
| `llm.cost.dollars` | Computed from model pricing table (see `usage/tracker.go:44`) | float |
| `llm.duration_ms` | `TurnResult.Duration` | float |
| `llm.success` | `err == nil` | bool |
| `llm.error` | Error message if failed | string |
| `llm.stop_reason` | `TurnResult.StopReason` (end_turn, tool_use, max_tokens) | string |
| `llm.stream` | `api.Request.Stream` | bool |
| `session.id` | Session UUID | string |
| `turn.index` | Sequential turn number in session | int |
| `turn.tool_calls` | Number of tool calls in this turn | int |
| `turn.tool_names` | Comma-separated tool names invoked | string |

**OTEL metric instruments** (in `instruments.go`):

| Instrument | Type | Unit | Description |
|------------|------|------|-------------|
| `ycode.llm.call.duration` | Float64Histogram | ms | LLM API call latency |
| `ycode.llm.call.total` | Int64Counter | 1 | Total LLM calls (labels: provider, model, success, stop_reason) |
| `ycode.llm.tokens.input` | Int64Counter | tokens | Input tokens consumed |
| `ycode.llm.tokens.output` | Int64Counter | tokens | Output tokens consumed |
| `ycode.llm.tokens.cache_read` | Int64Counter | tokens | Tokens served from cache |
| `ycode.llm.tokens.cache_write` | Int64Counter | tokens | Tokens written to cache |
| `ycode.llm.cost.dollars` | Float64Counter | USD | Estimated cumulative cost |
| `ycode.llm.context_window.used` | Int64Gauge | tokens | Tokens used in context window per call |
| `ycode.tool.call.duration` | Float64Histogram | ms | Tool execution latency (label: tool_name) |
| `ycode.tool.call.total` | Int64Counter | 1 | Tool invocations (labels: tool_name, success) |
| `ycode.turn.duration` | Float64Histogram | ms | Full turn duration (API + tools) |
| `ycode.turn.tool_count` | Int64Histogram | 1 | Tools invoked per turn |
| `ycode.session.turns` | Int64Counter | 1 | Turns per session |
| `ycode.compaction.total` | Int64Counter | 1 | Compaction events |
| `ycode.compaction.tokens_saved` | Int64Counter | tokens | Tokens reclaimed by compaction |

**Model pricing table** (in `instruments.go` or `cost.go`):
Extend the hardcoded pricing in `usage/tracker.go:44` into a lookup map keyed by model prefix, supporting Claude (Sonnet/Opus/Haiku), GPT-4o, GPT-4, etc. The `llm.cost.dollars` attribute is computed per-call.

### 1.3 Detailed tool call tracking (for self-healing/recovery)

The OTEL tool middleware (`middleware.go`) and conversation logger capture **full tool call details**:

**OTEL span per tool call** (`ycode.tool.call`):

| Attribute | Value |
|-----------|-------|
| `tool.name` | Tool name (e.g. `Bash`, `Read`, `Edit`, `Grep`) |
| `tool.source` | `builtin`, `plugin`, `mcp` |
| `tool.category` | `standard`, `llm`, `interactive` |
| `tool.input` | Full JSON input parameters (span event, not attribute — can be large) |
| `tool.input.summary` | Truncated first 512 chars of input for attribute-level queries |
| `tool.output` | Full output text (span event) |
| `tool.output.summary` | Truncated first 512 chars of output for attribute-level queries |
| `tool.output.size` | Output length in bytes |
| `tool.success` | bool |
| `tool.error` | Error message if failed |
| `tool.duration_ms` | Execution time |

**Span events** (attached to the tool span for full payload capture):
- `tool.input_received` event with `input` attribute containing the full `json.RawMessage`
- `tool.output_produced` event with `output` attribute containing the full output string
- `tool.error_occurred` event if the tool failed, with error details

**Why span events instead of attributes**: OTEL attributes have recommended size limits (~4KB). Tool inputs (e.g., file content for Write) and outputs (e.g., file contents from Read, grep results) can be very large. Span events allow unbounded payloads while keeping the span attributes searchable with summaries.

**Middleware implementation** (`middleware.go`):
```go
func ToolMiddleware(tracer trace.Tracer, meter metric.Meter) tools.Middleware {
    return func(toolName string) func(tools.ToolFunc) tools.ToolFunc {
        return func(next tools.ToolFunc) tools.ToolFunc {
            return func(ctx context.Context, input json.RawMessage) (string, error) {
                ctx, span := tracer.Start(ctx, "ycode.tool.call",
                    trace.WithAttributes(
                        attribute.String("tool.name", toolName),
                        attribute.String("tool.input.summary", truncate(string(input), 512)),
                    ))
                defer span.End()
                
                // Record full input as span event
                span.AddEvent("tool.input_received", trace.WithAttributes(
                    attribute.String("input", string(input)),
                ))
                
                start := time.Now()
                output, err := next(ctx, input)
                dur := time.Since(start)
                
                span.SetAttributes(
                    attribute.Int64("tool.duration_ms", dur.Milliseconds()),
                    attribute.Int("tool.output.size", len(output)),
                    attribute.String("tool.output.summary", truncate(output, 512)),
                    attribute.Bool("tool.success", err == nil),
                )
                
                // Record full output as span event
                span.AddEvent("tool.output_produced", trace.WithAttributes(
                    attribute.String("output", output),
                ))
                
                if err != nil {
                    span.RecordError(err)
                    span.SetStatus(codes.Error, err.Error())
                    span.AddEvent("tool.error_occurred", trace.WithAttributes(
                        attribute.String("error", err.Error()),
                    ))
                }
                
                // Record metrics
                toolDuration.Record(ctx, float64(dur.Milliseconds()),
                    metric.WithAttributes(attribute.String("tool_name", toolName)))
                toolTotal.Add(ctx, 1, metric.WithAttributes(
                    attribute.String("tool_name", toolName),
                    attribute.Bool("success", err == nil)))
                
                return output, err
            }
        }
    }
}
```

**Conversation log tool details** (in `ConversationRecord`):
```go
type ToolCallLog struct {
    Name       string          `json:"name"`
    Source     string          `json:"source"`       // builtin, plugin, mcp
    Input      json.RawMessage `json:"input"`        // full input parameters
    Output     string          `json:"output"`       // full output
    Error      string          `json:"error,omitempty"`
    Success    bool            `json:"success"`
    DurationMs int64           `json:"duration_ms"`
}
```

**Use cases enabled by full tool call tracking:**
- **Auto recovery**: If a tool fails, the agent can review the full input/output trace to understand what went wrong and retry with corrected parameters
- **Self-healing**: Patterns of failures (e.g., `Edit` failing due to non-unique `old_string`) can be detected from traces and fed back as context
- **Improvement**: Aggregate tool call patterns to identify inefficient tool usage (e.g., reading the same file multiple times, unnecessary glob before grep)
- **Debugging**: Full audit trail of every tool invocation with exact parameters for reproducing issues

### 1.4 Instrumentation points (detailed)

**`internal/api/anthropic.go` `Send()`** — Wrap in span `ycode.api.call`:
- Set all `llm.*` attributes from request before call, from response/usage after
- Record `ycode.llm.call.duration`, `ycode.llm.call.total`, token counters, cost counter
- On error: `span.RecordError(err)`, `span.SetStatus(codes.Error, ...)`

**`internal/api/openai_compat.go` `Send()`** — Same instrumentation as anthropic

**`internal/tools/registry.go` `Invoke()`** — OTEL middleware via `ApplyMiddleware`:
- Child span `ycode.tool.call` with full input/output capture (see 1.3 above)
- Record `ycode.tool.call.duration`, `ycode.tool.call.total`

**`internal/runtime/conversation/runtime.go` `Turn()`** — Span `ycode.conversation.turn`:
- Attrs: `turn.index`, `turn.tool_calls`, `turn.tool_names`
- Record `ycode.turn.duration`, `ycode.turn.tool_count`, `ycode.session.turns`
- Child spans: api call + each tool execution (automatic via context propagation)

**`internal/runtime/conversation/runtime.go` `TurnWithRecovery()`** — Span `ycode.compaction`:
- Attrs: `tokens.before`, `tokens.after`
- Record `ycode.compaction.total`, `ycode.compaction.tokens_saved`

### 1.5 slog bridge

Replace default slog handler with `otelslog.NewHandler` wrapping a `TeeHandler` that writes to both OTEL LoggerProvider and the original stderr handler. All existing `slog.Debug/Info/Error` calls become OTEL log records exported via OTLP.

---

## Phase 2: Local Disk Persistence (`~/.ycode/otel/`)

All telemetry data is persisted locally under `~/.ycode/otel/` for auditing and debugging, **independent of whether the collector/stack is running**.

### 2.1 Directory layout

```
~/.ycode/otel/
  logs/                               # Full conversation + tool call logs (JSONL, rotating)
    conversations-2026-04-11.jsonl
    conversations-2026-04-10.jsonl
    conversations-2026-04-09.jsonl
  traces/                             # OTLP trace export files (protobuf JSONL)
    traces-2026-04-11.jsonl
    traces-2026-04-10.jsonl
  metrics/                            # OTLP metric export files (protobuf JSONL)
    metrics-2026-04-11.jsonl
    metrics-2026-04-10.jsonl
```

### 2.2 Conversation logger (`internal/telemetry/otel/request_logger.go`)

New `RequestLogger` that captures full request/response payloads **including tool call details** for every API call:

```go
type RequestLogger struct {
    dir         string           // ~/.ycode/otel/logs/
    retention   time.Duration    // default 3 days
    mu          sync.Mutex
    currentFile *os.File
    currentDate string
}

// ConversationRecord is one JSONL line per API call
type ConversationRecord struct {
    Timestamp    time.Time       `json:"timestamp"`
    SessionID    string          `json:"session_id"`
    TurnIndex    int             `json:"turn_index"`
    Provider     string          `json:"provider"`
    Model        string          `json:"model"`
    
    // Request
    SystemPrompt string          `json:"system_prompt"`
    Messages     []api.Message   `json:"messages"`      // full user/assistant history sent
    ToolDefs     int             `json:"tool_defs"`      // count of tool definitions sent
    MaxTokens    int             `json:"max_tokens"`
    Temperature  *float64        `json:"temperature,omitempty"`
    
    // Response
    ResponseText    string       `json:"response_text"`
    ThinkingContent string       `json:"thinking_content,omitempty"`
    ToolCalls       []ToolCallLog `json:"tool_calls,omitempty"`
    StopReason      string       `json:"stop_reason"`
    
    // Usage & Cost
    TokensIn         int         `json:"tokens_in"`
    TokensOut        int         `json:"tokens_out"`
    CacheCreation    int         `json:"cache_creation"`
    CacheRead        int         `json:"cache_read"`
    EstimatedCostUSD float64     `json:"estimated_cost_usd"`
    DurationMs       int64       `json:"duration_ms"`
    Success          bool        `json:"success"`
    Error            string      `json:"error,omitempty"`
}

// ToolCallLog captures full tool invocation details for debugging and self-healing
type ToolCallLog struct {
    Name       string          `json:"name"`
    Source     string          `json:"source"`       // builtin, plugin, mcp
    Input      json.RawMessage `json:"input"`        // full input parameters
    Output     string          `json:"output"`       // full output text
    Error      string          `json:"error,omitempty"`
    Success    bool            `json:"success"`
    DurationMs int64           `json:"duration_ms"`
}
```

**Key behaviors:**
- One JSONL file per day: `conversations-YYYY-MM-DD.jsonl`
- Rotation: on startup and daily at midnight, delete files older than `retention` (default 3 days, configurable via `observability.logRetentionDays`)
- Called from `conversation.Runtime.Turn()` after tool execution completes — captures request, response, AND all tool call results in one record
- System prompt included verbatim for audit trail
- Messages array included (the full conversation context sent to the API)
- **Tool call inputs and outputs included in full** — enables post-hoc analysis, self-healing replay, and debugging

### 2.3 File-based OTLP exporters

In `provider.go`, configure **dual export** for all three signals:

1. **OTLP gRPC exporter** -> collector at `localhost:4317` (when collector is running)
2. **OTLP file exporter** -> `~/.ycode/otel/{traces,metrics}/` (always active)

Use `go.opentelemetry.io/otel/exporters/stdout/stdouttrace` and `stdoutmetric` writing to rotating files. Each file is date-stamped. The file exporter is wrapped in a `RotatingFileExporter` that:
- Creates a new file per day
- Deletes files older than retention period (default 3 days)
- Is non-blocking (async batch processor handles buffering)

### 2.4 Config additions

Add to `ObservabilityConfig`:

```go
type ObservabilityConfig struct {
    // ... existing fields ...
    
    // Local persistence
    DataDir          string `json:"dataDir"`          // default "~/.ycode/otel"
    LogRetentionDays int    `json:"logRetentionDays"` // default 3
    LogConversations bool   `json:"logConversations"` // default true when enabled
    LogToolDetails   bool   `json:"logToolDetails"`   // default true; log full tool input/output
    PersistTraces    bool   `json:"persistTraces"`    // default true when enabled
    PersistMetrics   bool   `json:"persistMetrics"`   // default true when enabled
}
```

### 2.5 Retention cleanup

New `internal/telemetry/otel/retention.go`:
- `CleanupOldFiles(dir string, maxAge time.Duration)` — scans date-stamped files, removes expired
- Called on startup and via a background ticker (once per hour)
- Operates independently on each subdirectory (logs/, traces/, metrics/)

---

## Phase 3: OpenTelemetry Collector Management

### New package: `internal/collector/`

| File | Purpose |
|------|---------|
| `manager.go` | `CollectorManager` — download, configure, start/stop/health check of otelcol subprocess. PID file at `~/.ycode/otel/collector/otelcol.pid`. All listeners bind to `127.0.0.1` on random ports. |
| `config.go` | Generate collector YAML from ycode config. Ports assigned dynamically (see Phase 4.5). |
| `download.go` | Download official `otelcol-contrib` binary for current OS/arch from GitHub releases. Store at `~/.ycode/bin/otelcol-{version}`. Checksum verification. |
| `embed.go` | Embedded default config YAML via `//go:embed`. |

### Collector config pipeline

```yaml
receivers:
  otlp:
    protocols:
      grpc: { endpoint: "127.0.0.1:0" }   # random port, registered with proxy
      http: { endpoint: "127.0.0.1:0" }   # random port, registered with proxy

processors:
  batch:
    timeout: 5s
  memory_limiter:
    limit_mib: 256

exporters:
  prometheus:
    endpoint: "127.0.0.1:0"               # random port, scraped by local Prometheus
  otlphttp/vlogs:
    endpoint: "http://127.0.0.1:{vlogs_port}/insert/opentelemetry"
  otlphttp/remote:                         # optional, if remoteWrite configured
    endpoint: "${REMOTE_OTLP_URL}"

extensions:
  health_check:
    endpoint: "127.0.0.1:0"               # random port

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch, memory_limiter]
      exporters: [otlphttp/vlogs]
    metrics:
      receivers: [otlp]
      processors: [batch, memory_limiter]
      exporters: [prometheus]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/vlogs]
```

**Note**: The collector's OTLP gRPC receiver port must be known to ycode's SDK exporter. The `CollectorManager` allocates the port first (via `net.Listen` on `:0`, then close), writes it to the config, starts the collector, and returns the port for the SDK exporter to use.

### Custom minimal collector (optional optimization)

Add `configs/otelcol/builder-config.yaml` for OCB to build a ~30MB binary instead of ~200MB contrib. Add `make collector` target.

---

## Phase 4: Built-in Prometheus Stack with Reverse Proxy

### 4.1 Architecture: Reverse proxy on fixed port, services on random localhost ports

```
User browser -> http://localhost:58080/
                    |
                    v
            ┌─────────────────────────────┐
            │  Reverse Proxy (Go stdlib)  │
            │  127.0.0.1:58080            │
            └──────┬──────────────────────┘
                   │  path-based routing
                   │
    ┌──────────────┼──────────────────────────────────┐
    │              │              │          │         │
    v              v              v          v         v
 /prometheus/   /alerts/      /karma/   /dashboard/ /logs/
 127.0.0.1:R1  127.0.0.1:R2  127.0.0.1:R3  R4      R5
 Prometheus    Alertmanager   Karma      Perses   VictoriaLogs

 R1-R5 = random ephemeral ports, localhost-only
```

**Key design:**
- Single fixed entry point: `127.0.0.1:58080` (configurable via `observability.proxyPort`)
- All backend services bind to `127.0.0.1` on random ports (OS-assigned via `:0`)
- Backends are **not directly accessible** from non-localhost (they bind `127.0.0.1`, not `0.0.0.0`)
- The reverse proxy is also localhost-only by default
- Path-based routing with prefix stripping where needed

### 4.2 New package: `internal/observability/`

| File | Purpose |
|------|---------|
| `process.go` | Generic `Process` struct for child process lifecycle: start on random `127.0.0.1` port, stop, healthy (HTTP health check), PID file, log capture. Returns assigned port after startup. |
| `proxy.go` | **Reverse proxy server** using `net/http` + `net/http/httputil.ReverseProxy`. Path routing, prefix stripping, health aggregation endpoint at `/healthz`. |
| `stack.go` | `StackManager` orchestrating all components + proxy. `Start()`, `Stop()`, `Status()`, `EnsureBinaries()`. |
| `ports.go` | `PortAllocator` — allocates random free ports via `net.Listen("tcp", "127.0.0.1:0")`, records allocations in `~/.ycode/observability/ports.json` for discoverability. |
| `prometheus.go` | Prometheus config gen + process management. Binds `127.0.0.1:{random}`. Scrape targets use allocated ports. |
| `alertmanager.go` | Alertmanager config gen. Binds `127.0.0.1:{random}`. |
| `karma.go` | Karma config gen pointing at local alertmanager by allocated port. |
| `perses.go` | Perses config gen with pre-built ycode dashboards. |
| `victorialogs.go` | VictoriaLogs config gen. Binds `127.0.0.1:{random}`. |
| `download.go` | Binary downloader for all components. Version manifest at `~/.ycode/bin/versions.json`. |
| `gateway.go` | Generate remote-write and federation YAML sections from config, with env var expansion for secrets. |

### 4.3 Reverse proxy (`internal/observability/proxy.go`)

```go
type ProxyServer struct {
    listenAddr string            // "127.0.0.1:58080"
    routes     map[string]*url.URL // path prefix -> backend URL
    server     *http.Server
}

func NewProxyServer(port int) *ProxyServer

// AddRoute registers a backend. Example: AddRoute("/prometheus/", "http://127.0.0.1:39821")
func (p *ProxyServer) AddRoute(pathPrefix string, backend *url.URL)

func (p *ProxyServer) Start(ctx context.Context) error
func (p *ProxyServer) Stop(ctx context.Context) error
```

**Route table:**

| Path prefix | Backend | Notes |
|-------------|---------|-------|
| `/prometheus/` | Prometheus `--web.external-url` + `--web.route-prefix` | Prometheus supports sub-path natively |
| `/alerts/` | Alertmanager `--web.external-url` + `--web.route-prefix` | Alertmanager supports sub-path natively |
| `/karma/` | Karma `--alertmanager.uri` | Karma supports `--listen.prefix` |
| `/dashboard/` | Perses | Perses supports base path config |
| `/logs/` | VictoriaLogs | VictoriaLogs supports `-httpListenAddr` |
| `/collector/` | OTEL Collector health/zpages | Health check and debug endpoints |
| `/healthz` | Aggregated health check | Returns JSON with status of each component |
| `/` | Landing page | Simple HTML page with links to all services |

**Landing page** (`/`): A minimal HTML page (embedded via `//go:embed`) showing:
- Links to each service dashboard
- Component health status (green/red indicators)
- ycode version and session info

### 4.4 Port allocation (`internal/observability/ports.go`)

```go
type PortAllocator struct {
    mu    sync.Mutex
    ports map[string]int // component name -> allocated port
    path  string         // ~/.ycode/observability/ports.json
}

// Allocate finds a free port on 127.0.0.1, records it, returns the port number.
func (pa *PortAllocator) Allocate(component string) (int, error)

// Get returns the port for a component (from ports.json), or 0 if not allocated.
func (pa *PortAllocator) Get(component string) int

// Release removes a port allocation.
func (pa *PortAllocator) Release(component string)
```

Ports are persisted to `~/.ycode/observability/ports.json` so that:
- `ycode observe status` can show port mappings even from a different terminal
- Config generation for cross-references (e.g., Prometheus scraping collector) uses allocated ports
- The OTEL SDK exporter knows which port the collector's gRPC receiver is on

### 4.5 Startup sequence

1. `PortAllocator.Allocate()` for each component (collector-grpc, collector-http, collector-prom, collector-health, prometheus, alertmanager, karma, perses, vlogs)
2. Generate configs for all components using allocated ports
3. Start components in dependency order:
   - VictoriaLogs first (log sink)
   - OTEL Collector (needs VictoriaLogs port)
   - Prometheus (needs collector prometheus-exporter port)
   - Alertmanager
   - Karma (needs alertmanager port)
   - Perses
4. Start reverse proxy on fixed port (default `58080`)
5. Write `ports.json` with all allocations
6. Health-check loop confirms all components responding

### 4.6 Config extension

```go
type ObservabilityConfig struct {
    Enabled        bool    `json:"enabled"`          // default false
    CollectorAddr  string  `json:"collectorAddr"`    // auto-set from allocated port when stack running
    SampleRate     float64 `json:"sampleRate"`       // default 1.0

    StackEnabled   bool    `json:"stackEnabled"`     // auto-start obs stack
    ProxyPort      int     `json:"proxyPort"`        // default 58080, the single fixed port
    ProxyBindAddr  string  `json:"proxyBindAddr"`    // default "127.0.0.1" (localhost only)

    // Remote gateway
    RemoteWrite []RemoteWriteTarget `json:"remoteWrite,omitempty"`
    Federation  []FederationTarget  `json:"federation,omitempty"`

    // Local persistence
    DataDir          string `json:"dataDir"`          // default "~/.ycode/otel"
    LogRetentionDays int    `json:"logRetentionDays"` // default 3
    LogConversations bool   `json:"logConversations"` // default true when enabled
    LogToolDetails   bool   `json:"logToolDetails"`   // default true; log full tool input/output
    PersistTraces    bool   `json:"persistTraces"`    // default true when enabled
    PersistMetrics   bool   `json:"persistMetrics"`   // default true when enabled
}

type RemoteWriteTarget struct {
    URL       string            `json:"url"`
    Headers   map[string]string `json:"headers,omitempty"`
    BasicAuth *BasicAuth        `json:"basicAuth,omitempty"`
}

type FederationTarget struct {
    URL   string   `json:"url"`
    Match []string `json:"match"`
}
```

**Removed**: Individual port configs for prometheus/alertmanager/karma/perses/vlogs — they're all random now. Only `proxyPort` (the user-facing entry point) is configurable.

### Default config templates: `configs/`

```
configs/
  otelcol/default.yaml
  prometheus/default.yml
  prometheus/alerts/ycode.yml       # error rate, slow API, token budget, cost alerts
  alertmanager/default.yml
  karma/default.yaml
  perses/default.yaml
  proxy/landing.html                # embedded landing page template
```

### Full data layout

```
~/.ycode/
  bin/                              # downloaded binaries
    otelcol-0.115.0-darwin-arm64
    prometheus-3.1.0-darwin-arm64
    alertmanager-0.28.0-darwin-arm64
    karma-0.120-darwin-arm64
    perses-0.50.0-darwin-arm64
    victorialogs-1.0.0-darwin-arm64
    versions.json
  otel/                             # local telemetry persistence
    logs/                           # rotating conversation + tool JSONL (3-day default)
      conversations-YYYY-MM-DD.jsonl
    traces/                         # rotating OTLP trace files
      traces-YYYY-MM-DD.jsonl
    metrics/                        # rotating OTLP metric files
      metrics-YYYY-MM-DD.jsonl
    collector/                      # collector runtime
      config.yaml
      otelcol.pid
  observability/                    # stack runtime
    ports.json                      # allocated port mappings
    prometheus/config.yml data/
    alertmanager/config.yml
    karma/config.yaml
    perses/config.yaml
    vlogs/config.yaml data/
```

---

## Phase 5: CLI Commands

Add `ycode observe` subcommand tree in `cmd/ycode/main.go`:

| Command | Action |
|---------|--------|
| `ycode observe start` | Download binaries, allocate ports, generate configs, start all + proxy |
| `ycode observe stop` | SIGTERM all managed processes, cleanup PID files, release ports |
| `ycode observe status` | Table: component, PID, port, proxy path, health. Shows proxy URL. |
| `ycode observe logs [component]` | Tail log file for a specific component |
| `ycode observe dashboard` | Open `http://localhost:58080/` in browser (landing page) |
| `ycode observe config` | Print generated configs and port allocations |
| `ycode observe download` | Pre-download all binaries |
| `ycode observe reset` | Remove all observability data after confirmation |
| `ycode observe audit [--last N]` | Show recent conversation records from `~/.ycode/otel/logs/` |

Example `ycode observe status` output:
```
Observability Stack — http://127.0.0.1:58080/
COMPONENT       PID     PORT    PROXY PATH      HEALTH
otel-collector  12345   41823   /collector/     healthy
prometheus      12346   39821   /prometheus/    healthy
alertmanager    12347   42156   /alerts/        healthy
karma           12348   38901   /karma/         healthy
perses          12349   40112   /dashboard/     healthy
victorialogs    12350   43567   /logs/          healthy
proxy           12351   58080   /               healthy
```

---

## Phase 6: Dashboards and Alert Rules

### Perses dashboards (JSON in `configs/perses/`)

**LLM Performance dashboard:**
- `ycode.llm.call.duration` histogram (p50/p95/p99) by model
- `ycode.llm.call.total` rate by provider, model, success
- `ycode.llm.cost.dollars` cumulative and rate
- `ycode.llm.tokens.input/output` rate by model
- `ycode.llm.tokens.cache_read / (cache_read + input)` — cache hit ratio
- `ycode.llm.context_window.used` gauge — how full the context window is

**Tool Usage dashboard:**
- `ycode.tool.call.duration` heatmap by tool_name
- `ycode.tool.call.total` top-N tools by invocation count
- Tool error rate: `rate(total{success=false}) / rate(total)`
- `ycode.turn.tool_count` histogram — tools per turn distribution

**Session Overview dashboard:**
- `ycode.session.turns` counter per session
- `ycode.compaction.total` rate — how often compaction fires
- `ycode.compaction.tokens_saved` — tokens reclaimed
- `ycode.turn.duration` — end-to-end turn latency

**Cost dashboard:**
- `ycode.llm.cost.dollars` cumulative by model
- Projected daily/weekly cost extrapolation
- Per-session cost breakdown

### Prometheus alerts (`configs/prometheus/alerts/ycode.yml`)
- `YcodeAPIErrorRate` > 10% over 5m
- `YcodeAPILatencyP99` > 30s
- `YcodeToolFailureRate` > 25% over 5m
- `YcodeTokenBudgetExhausted` context window > 90% full
- `YcodeCostThreshold` estimated cost > configurable $/hour
- `YcodeCompactionFrequency` > 3 compactions per session (suggests prompt bloat)

---

## Phase 7: Startup Wiring (`cmd/ycode/main.go`)

In `newApp()`, after storage init:

```go
if cfg.Observability != nil && cfg.Observability.Enabled {
    otelDataDir := cfg.Observability.DataDir // default ~/.ycode/otel
    
    // 1. Request logger (always writes to disk, includes full tool details)
    reqLogger := otel.NewRequestLogger(otelDataDir, otel.RequestLoggerConfig{
        RetentionDays:  cfg.Observability.LogRetentionDays,
        LogToolDetails: cfg.Observability.LogToolDetails,
    })
    
    // 2. Resolve collector address
    //    If stack is running, read port from ports.json
    //    Otherwise use configured collectorAddr (or skip gRPC export)
    collectorAddr := resolveCollectorAddr(cfg.Observability)
    
    // 3. Create OTEL provider with dual export (file + gRPC)
    otelProvider, err := otel.NewProvider(ctx, otel.Config{
        CollectorAddr:  collectorAddr,
        ServiceName:    "ycode",
        ServiceVersion: version,
        SessionID:      sess.ID,
        SampleRate:     cfg.Observability.SampleRate,
        DataDir:        otelDataDir,
        PersistTraces:  cfg.Observability.PersistTraces,
        PersistMetrics: cfg.Observability.PersistMetrics,
    })
    
    // 4. Wire OTELSink into existing telemetry
    otelSink := otel.NewOTELSink(otelProvider)
    
    // 5. Apply OTEL tool middleware (captures full input/output)
    otelMW := otel.NewToolMiddleware(otelProvider)
    otelMW.ApplyToRegistry(toolReg)
    
    // 6. Wire request logger into conversation Runtime
    // 7. Start retention cleanup goroutine
    // 8. Register shutdown
    
    // Auto-start stack if configured
    if cfg.Observability.StackEnabled {
        go stackMgr.Start(ctx)
    }
}
```

---

## Critical files to modify

| File | Change |
|------|--------|
| `go.mod` | Add OTEL SDK + gRPC + stdout exporter dependencies |
| `internal/runtime/config/config.go` | Add `ObservabilityConfig` struct with all fields, merge logic |
| `cmd/ycode/main.go` | Wire OTEL provider + request logger, add `observe` command tree |
| `internal/runtime/conversation/runtime.go` | Add `RequestLogger` field; after `Turn()` + `ExecuteTools()`, log full record with tool call details |
| `internal/api/anthropic.go` | Add OTEL span wrapping `Send()` with all `llm.*` attributes |
| `internal/api/openai_compat.go` | Same OTEL instrumentation as anthropic |
| `internal/telemetry/sink.go` | No changes (Sink interface reused as-is) |
| `internal/tools/metrics.go` | No changes (coexists with OTEL middleware) |
| `internal/runtime/usage/tracker.go` | Extract pricing map into a shared `cost.go` for reuse by OTEL instruments |
| `Makefile` | Add `collector` target |

## New files

- `internal/telemetry/otel/` — 7 files (provider, attributes, bridge, middleware, request_logger, instruments, shutdown) + retention.go
- `internal/collector/` — 4 files (manager, config, download, embed)
- `internal/observability/` — 11 files (process, proxy, ports, stack, prometheus, alertmanager, karma, perses, victorialogs, download, gateway)
- `configs/` — 7+ config templates + landing.html

---

## Verification

1. **Unit tests**: Each new package gets `_test.go` files. `OTELSink` tested with in-memory exporter. `RequestLogger` tested with temp dir. `RotatingFileExporter` tested for rotation/retention. `PortAllocator` tested for uniqueness and persistence. `ProxyServer` tested with httptest backends.
2. **Tool call logging test**: Run a prompt that triggers tool calls (e.g., `Read` a file), verify `conversations-YYYY-MM-DD.jsonl` entry contains `tool_calls[].input` with the file path and `tool_calls[].output` with the file content.
3. **OTEL trace test**: Verify tool call spans contain `tool.input_received` and `tool.output_produced` events with full payloads, and `tool.input.summary`/`tool.output.summary` attributes with truncated versions.
4. **Metrics test**: Run a prompt, verify `~/.ycode/otel/metrics/` files contain `ycode.llm.call.duration`, `ycode.llm.cost.dollars`, `ycode.tool.call.duration` with correct labels.
5. **Retention test**: Set `logRetentionDays: 1`, create old files, verify cleanup removes them.
6. **Proxy test**: `ycode observe start` -> verify `http://127.0.0.1:58080/` returns landing page -> `/prometheus/`, `/dashboard/`, `/karma/`, `/alerts/`, `/logs/` all proxy correctly -> direct access to random ports works only from localhost.
7. **Port isolation test**: Verify all backend services bind to `127.0.0.1` (not `0.0.0.0`) by checking `netstat`/`lsof` output.
8. **Integration test**: Full flow: start stack -> send prompt -> see traces in collector -> metrics in Prometheus via proxy -> `ycode observe stop` cleans up all processes.
9. **Build**: `make build` still produces single binary. `make test` passes. `make vet` clean.
10. **Remote gateway**: Configure a `remoteWrite` target, verify Prometheus forwards metrics.

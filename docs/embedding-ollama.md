# Embedding Ollama

How Ollama is embedded in ycode as the local inference engine. This is the canonical pattern that Podman and Gitea embedding follow.

## Architecture

Ollama is embedded as a **managed external process** — the Go management layer is compiled into the ycode binary, while the C++ inference runner runs as a separate process managed by ycode.

```
ycode binary
├── external/ollama/           ← git submodule (upstream: ollama/ollama)
│   └── embed/embed.go         ← pure-Go wrapper, re-exports API types
├── internal/inference/
│   ├── ollama.go              ← OllamaComponent (observability.Component)
│   ├── runner.go              ← RunnerManager (process lifecycle)
│   ├── otel.go                ← OTEL instrumentation
│   ├── ui.go                  ← OllamaUIComponent (auto-detect standalone)
│   ├── web/embed.go           ← embedded SPA static assets
│   └── huggingface.go         ← model discovery + import
└── cmd/ycode/serve.go         ← registration in buildStackManager()
```

## Submodule Pattern

```
go.mod:   replace github.com/ollama/ollama => ./external/ollama
import:   "github.com/ollama/ollama/embed" → resolves to external/ollama/embed/
```

The `embed` package deliberately avoids importing Ollama's `server/`, `discover/`, `llm/`, `llama/`, and `x/` packages which require CGo. It re-exports only pure-Go packages: API types, model management, manifest handling, template rendering, and configuration.

## Component Interface

`OllamaComponent` implements `observability.Component`:

```go
type Component interface {
    Name() string
    Start(ctx context.Context) error   // non-blocking
    Stop(ctx context.Context) error    // graceful shutdown
    Healthy() bool
    HTTPHandler() http.Handler         // mounted on reverse proxy
}
```

Additional interfaces: `Port() int` for reverse proxying, `BaseURL() string`, `Runner() *RunnerManager` for direct access.

## RunnerManager Lifecycle

The RunnerManager handles the external C++ inference binary:

1. **Binary discovery** (4-tier priority):
   - Explicit `runnerPath` from config
   - `$OLLAMA_RUNNERS` environment variable
   - Adjacent to ycode binary: `$(dirname ycode)/ollama`
   - System PATH: `which ollama`

2. **Startup**: Allocate ephemeral port → spawn `ollama serve` with `OLLAMA_HOST=127.0.0.1:{port}` → poll health endpoint with exponential backoff

3. **Monitoring**: Background goroutine watches for unexpected exit → auto-restart up to 3 times with exponential backoff (1s, 2s, 4s)

4. **Callbacks**: `OnCrash(err)` and `OnRestart()` for OTEL event tracing

## OTEL Instrumentation

- **Gauges**: `ycode.inference.runner.healthy`, `ycode.inference.runner.restart_count`, `ycode.inference.runner.port`
- **Spans**: `ycode.inference.runner.start`, `ycode.inference.runner.crash`, `ycode.inference.runner.health_check`
- **Counters**: `InferenceRunnerStarts`, `InferenceRunnerCrashes`

Configured via `SetOTEL(cfg *OTELConfig)` before `Start()`.

## StackManager Registration

In `cmd/ycode/serve.go` `buildStackManager()`:

```go
var ollamaComp *inference.OllamaComponent
if inferCfg != nil && inferCfg.Enabled {
    ollamaComp = inference.NewOllamaComponent(inferCfg, filepath.Join(dataDir, "inference"))
    mgr.AddComponent(ollamaComp)
} else {
    mgr.AddComponent(inference.NewOllamaUIComponent())
}
```

When inference is disabled, `OllamaUIComponent` auto-detects standalone Ollama servers and provides the management UI without managing the runner.

Proxy path: `/ollama/` — API requests proxied to runner, everything else served from embedded SPA.

## Configuration

```json
{
  "inference": {
    "enabled": true,
    "runnerPath": "/path/to/ollama",
    "autoDownload": true,
    "defaultModel": "llama3",
    "modelsDir": "/custom/models",
    "gpuLayers": -1,
    "maxVramMB": 8192
  }
}
```

Three-tier merge: user (`~/.config/ycode/settings.json`) > project (`.agents/ycode/settings.json`) > local (`settings.local.json`).

## Key Design Decisions

- **Pure-Go boundary**: The embed package re-exports only types that compile without CGo. The C++ inference binary is a separate process.
- **Ephemeral ports**: All internal ports are dynamically allocated — no conflicts when running multiple instances.
- **Graceful degradation**: If the runner crashes, health status updates immediately, auto-restart is attempted, and OTEL traces capture the event.
- **Composite HTTP handler**: API paths proxy to runner, UI paths serve embedded SPA — single mount point on the reverse proxy.

## Files

| File | Purpose |
|------|---------|
| `external/ollama/embed/embed.go` | Pure-Go API wrapper (re-exports types) |
| `internal/inference/ollama.go` | OllamaComponent (Component interface) |
| `internal/inference/runner.go` | RunnerManager (process lifecycle, health, restart) |
| `internal/inference/otel.go` | OTEL gauges, spans, metrics |
| `internal/inference/ui.go` | OllamaUIComponent (auto-detect standalone) |
| `internal/inference/web/embed.go` | Embedded management SPA |
| `internal/inference/huggingface.go` | HuggingFace model discovery + GGUF import |
| `internal/runtime/config/config.go` | `InferenceConfig` struct |
| `cmd/ycode/serve.go` | Registration in `buildStackManager()` |

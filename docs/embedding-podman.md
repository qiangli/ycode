# Embedding Podman

How Podman is embedded in ycode as the container isolation engine for agent swarms. Follows the Ollama embedding pattern.

## Goal

Run third-party code seamlessly within ycode for swarms of agents collaborating on common tasks. Each agent runs in an isolated container with shared access to the internal Ollama LLM and the OTEL observability stack for diagnosis.

## Architecture

ycode IS a podman implementation — it embeds Podman's Go container management layer directly and orchestrates OCI runtimes (runc/crun) for low-level container operations. The C dependencies (libseccomp, libgpgme) live in the OCI runtime, not in ycode.

```
ycode binary
├── external/podman/           ← git submodule (upstream: qiangli/podman)
│   └── embed/embed.go         ← pure-Go wrapper, re-exports pkg/bindings API
├── internal/container/
│   ├── engine.go              ← Engine (binary discovery, socket, service lifecycle)
│   ├── container.go           ← Container CRUD (create/start/stop/exec/remove)
│   ├── network.go             ← Session bridge network, host gateway
│   ├── image.go               ← Pull/build/list + embedded Dockerfile.sandbox
│   ├── pod.go                 ← Pod management for grouping agents
│   ├── component.go           ← ContainerComponent (observability.Component)
│   ├── otel.go                ← OTEL instrumentation
│   ├── pool.go                ← Pre-warmed container pool
│   ├── cleanup.go             ← Orphan cleanup (label-based)
│   └── Dockerfile.sandbox     ← Default agent sandbox image
├── internal/runtime/bash/
│   └── executor.go            ← Executor interface (Host + Container impls)
└── cmd/ycode/
    └── podman.go              ← CLI: ycode podman/docker subcommands
```

## Submodule Pattern

```
go.mod:   replace go.podman.io/podman/v6 => ./external/podman
import:   "go.podman.io/podman/v6/embed" → resolves to external/podman/embed/
```

The embed package re-exports `pkg/bindings` (pure-Go REST API client) for containers, images, networks, pods, volumes, and system operations. Anchor import in `internal/container/engine.go` ensures dependencies survive `go mod tidy`.

## Engine

The `Engine` wraps the Podman binary and provides container lifecycle operations via CLI exec:

1. **Binary discovery** (4-tier priority):
   - Explicit `binaryPath` from config
   - `$YCODE_CONTAINER_RUNTIME` environment variable
   - Adjacent to ycode binary: `$(dirname ycode)/podman`
   - System PATH: `which podman`

2. **Socket discovery** (checks in order):
   - Explicit `socketPath` from config
   - `$CONTAINER_HOST` environment variable
   - macOS podman machine socket: `$TMPDIR/podman/podman-machine-default-api.sock`
   - Linux XDG runtime dir: `$XDG_RUNTIME_DIR/podman/podman.sock`
   - Fallback: `podman info --format '{{.Host.RemoteSocket.Path}}'`

3. **Service startup**: If no existing socket found, starts `podman system service --timeout=0 unix://{dataDir}/podman.sock` and polls until socket accepts connections.

## ContainerComponent

Implements `observability.Component` and manages the full lifecycle:

- **Start**: Discover engine → cleanup orphans → create session network → build sandbox image (background) → warm pool (if configured)
- **Stop**: Drain pool → remove tracked containers → cleanup session → close engine
- **HTTPHandler**: Management API at `/containers/` with status and container list endpoints

### Service Environment Injection

Containers get environment variables to reach host services:

```
OLLAMA_HOST=http://host.containers.internal:{ollamaPort}
OTEL_EXPORTER_OTLP_ENDPOINT=http://host.containers.internal:{collectorGRPCPort}
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
YCODE_PROXY_URL=http://host.containers.internal:{proxyPort}
YCODE_SESSION_ID={sessionID}
```

Port wiring happens in `buildStackManager()` via `SetServicePorts()`.

### Security Defaults

Every agent container gets:
- `--read-only` root filesystem
- `--tmpfs /tmp,/var/tmp,/run` for writable scratch
- `--cap-drop=ALL` capability dropping
- `--init` for signal handling and zombie reaping
- `--label ycode.session={id}` for cleanup tracking
- Resource limits from config (CPU, memory)
- Bridge network per session (not host networking)

## Executor Abstraction

`internal/runtime/bash/executor.go` provides transparent containerized execution:

```go
type Executor interface {
    Execute(ctx context.Context, params ExecParams) (*ExecResult, error)
}

type HostExecutor struct{}           // current host behavior
type ContainerExecutor struct {      // delegates to container.Container.Exec()
    Container *container.Container
}
```

The conversation spawner injects `ContainerExecutor` for containerized agents — tool handlers (bash.go, etc.) don't change.

## Container Pool

Pre-warmed containers for reduced cold-start latency:
- `Warm(ctx)`: pre-create containers up to configured pool size
- `Acquire(ctx)`: claim a warm container, auto-replenish in background
- `Release(ctr)`: return container to pool (or remove if pool full)
- `Close(ctx)`: drain all containers

## CLI: `ycode podman` / `ycode docker`

10 subcommands (aliased as `ycode docker`):
- `ps [-a]` — list containers
- `images` — list images
- `pull IMAGE` — pull from registry
- `exec CONTAINER COMMAND` — execute in running container
- `logs [-f] [--tail N] CONTAINER` — fetch logs
- `stop CONTAINER` — stop container
- `rm [-f] CONTAINER` — remove container
- `run [--rm] [-d] IMAGE [CMD]` — run new container
- `version` — display podman version
- `inspect CONTAINER` — detailed container info

## OTEL Instrumentation

- **Gauges**: `ycode.container.active`, `ycode.container.pool.available`, `ycode.container.pool.total`
- **Counters**: `ycode.container.creates`, `ycode.container.execs`, `ycode.container.failures`
- **Spans**: component start/stop, container create/remove with attributes (agent ID, container name, image)

## Configuration

```json
{
  "container": {
    "enabled": true,
    "binaryPath": "/usr/bin/podman",
    "socketPath": "",
    "image": "ycode-sandbox:latest",
    "network": "bridge",
    "readOnlyRoot": true,
    "poolSize": 3,
    "cpus": "2.0",
    "memory": "4g"
  }
}
```

## Testing

- **Unit tests**: Engine discovery, config validation, component lifecycle, pool, image embed, network, pod, cleanup, CLI flag/arg validation
- **Integration tests** (`//go:build integration`): Engine connect, image pull/list, full container lifecycle (create/start/exec/stop/remove), network CRUD, container list — requires podman
- **Makefile**: `make test-container`

## Prior Art Studied

- **openclaw** (`priorart/openclaw/`): Docker sandbox with read-only root, tmpfs, cap drop, network isolation, volume mounts
- **OpenHands** (`priorart/openhands/`): Pluggable sandbox backends (Docker/Process/Remote), health check polling, session API keys, env var forwarding, `host.docker.internal` for container-to-host networking

## Future Work

- Deeper integration via `pkg/bindings` API (replace CLI exec where possible)
- Agent-type-aware workspace mounting: read-only for Explore/Plan, git worktree for write agents
- Container-aware conversation spawner: create container before agent loop, tear down on completion

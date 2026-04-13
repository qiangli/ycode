# Client/Server Architecture Plan

## Problem

ycode's TUI (`internal/cli/tui.go`) is tightly coupled to the backend. `TUIModel` holds a direct `*App` pointer and calls `RunPrompt()`, `ExecuteTools()`, and conversation runtime methods in-process. This means:

- No way to build a web UI or other frontends
- No programmatic API for agent operations
- No remote access to a running agent
- Testing the conversation pipeline requires wiring up the full CLI

## Goal

Introduce an HTTP/WebSocket server between the backend and frontends, with an event bus for real-time streaming and NATS for distributed messaging. The TUI becomes a client (in-process or remote), and a web UI can connect to the same server.

```
 ┌──────────┐     ┌──────────┐     ┌──────────────┐
 │   TUI    │     │  Web UI  │     │ Remote Client │
 │(bubbletea│     │(browser) │     │  (TUI/Web)    │
 └────┬─────┘     └────┬─────┘     └──────┬────────┘
      │                 │                   │
      │ In-process      │ WebSocket         │ NATS
      │                 │                   │
 ┌────▼─────────────────▼───────────────────▼─────┐
 │              HTTP + WebSocket Server            │
 │              (internal/server/)                 │
 ├─────────────────────────────────────────────────┤
 │              Service Layer                      │
 │              (internal/service/)                │
 ├────────────────────┬────────────────────────────┤
 │   MemoryBus        │       NATSBus              │
 │   (local)          │       (distributed)        │
 │   internal/bus/    │       internal/bus/nats.go  │
 ├────────────────────┴────────────────────────────┤
 │  Conversation Runtime                           │
 │  Provider │ Tools │ Session │ Storage            │
 └─────────────────────────────────────────────────┘
```

## Design

### New Packages

| Package | Purpose |
|---------|---------|
| `internal/bus/` | Event bus interface + MemoryBus + NATSBus implementations |
| `internal/service/` | Service layer — contract between server and backend |
| `internal/server/` | HTTP + WebSocket server (routes, handlers, middleware) |
| `internal/client/` | Go client for the ycode API (in-process + WebSocket + NATS) |
| `internal/web/` | Embedded static web UI assets |

### Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/gorilla/websocket` | WebSocket server and client |
| `github.com/nats-io/nats.go` | NATS client for distributed messaging |

### Event Bus (`internal/bus/`)

The bus interface has two implementations: `MemoryBus` for in-process communication and `NATSBus` for distributed/remote scenarios.

```go
type EventType string

const (
    EventTurnStart     EventType = "turn.start"
    EventTextDelta     EventType = "text.delta"
    EventThinkingDelta EventType = "thinking.delta"
    EventToolUseStart  EventType = "tool_use.start"
    EventToolProgress  EventType = "tool.progress"
    EventToolResult    EventType = "tool.result"
    EventTurnComplete  EventType = "turn.complete"
    EventTurnError     EventType = "turn.error"
    EventPermissionReq EventType = "permission.request"
    EventPermissionRes EventType = "permission.response"
    EventUsageUpdate   EventType = "usage.update"
    EventSessionUpdate EventType = "session.update"
    EventMessageSend   EventType = "message.send"
    EventTurnCancel    EventType = "turn.cancel"
)

type Event struct {
    ID        uint64          `json:"id"`
    Type      EventType       `json:"type"`
    SessionID string          `json:"session_id"`
    Timestamp time.Time       `json:"timestamp"`
    Data      json.RawMessage `json:"data"`
}

type Bus interface {
    Publish(event Event)
    Subscribe(filter ...EventType) (ch <-chan Event, unsubscribe func())
    Close() error
}
```

**MemoryBus** — `sync.RWMutex`-guarded subscriber list. Each subscriber gets a buffered channel (buffer size ~256). Slow consumers are dropped (non-blocking send). Monotonic event ID. Ring buffer (last 1024 events per session) for WebSocket reconnection replay.

**NATSBus** — Maps event types to NATS subjects:
- `ycode.events.{session_id}.{event_type}` — per-session events
- `ycode.events.*.{event_type}` — wildcard subscribe across sessions
- `ycode.events.{session_id}.>` — all events for a session

Events are JSON-serialized to NATS messages. Subscribe uses NATS subscriptions with Go channel delivery. NATS JetStream can optionally be used for persistence/replay, but plain NATS pub/sub is sufficient for the initial implementation.

### Service Layer (`internal/service/`)

The central abstraction that both the server and in-process TUI call. Extracted from the current `cli.App` facade.

```go
type Service interface {
    // Session lifecycle
    CreateSession(ctx context.Context) (*SessionInfo, error)
    GetSession(ctx context.Context, id string) (*SessionInfo, error)
    ListSessions(ctx context.Context) ([]SessionInfo, error)

    // Conversation — async, results arrive via bus events
    SendMessage(ctx context.Context, sessionID string, input MessageInput) error
    CancelTurn(ctx context.Context, sessionID string) error

    // Permission prompt response
    RespondPermission(ctx context.Context, requestID string, allowed bool) error

    // Config and state
    GetConfig(ctx context.Context) (*config.Config, error)
    SwitchModel(ctx context.Context, model string) error
    GetStatus(ctx context.Context) (*StatusInfo, error)

    // Slash commands
    ExecuteCommand(ctx context.Context, name string, args string) (string, error)

    // Event bus access
    Bus() bus.Bus
}
```

`LocalService` wraps the existing `cli.App` components (config, provider, session, tool registry, conversation runtime). The agentic loop currently in `App.RunPrompt()` (lines 170-282 of `app.go`) moves into `LocalService.SendMessage()`, which runs the loop as a goroutine and publishes events to the bus at each step.

### HTTP + WebSocket Server (`internal/server/`)

Uses Go 1.22+ `net/http.ServeMux` for REST routes and `gorilla/websocket` for the conversation channel.

**REST Routes** (stateless request/response):

```
GET    /api/health                        Health check
GET    /api/config                        Get merged config
PUT    /api/config/model                  Switch model

GET    /api/sessions                      List sessions
POST   /api/sessions                      Create new session
GET    /api/sessions/{id}                 Get session info
GET    /api/sessions/{id}/messages        Get message history

POST   /api/commands/{name}               Execute slash command

GET    /                                  Web UI (embedded SPA)
```

**WebSocket Route** (bidirectional conversation channel):

```
GET    /api/sessions/{id}/ws              Upgrade to WebSocket
```

The WebSocket connection carries all real-time conversation traffic for a session. No need for separate POST endpoints for sending messages, responding to permissions, or canceling turns — these are all WebSocket messages.

**WebSocket Protocol:**

Client → Server messages:
```json
{"type": "message.send", "data": {"text": "explain this code", "files": []}}
{"type": "permission.respond", "data": {"request_id": "abc-123", "allowed": true}}
{"type": "turn.cancel", "data": {}}
```

Server → Client messages:
```json
{"type": "turn.start", "data": {"turn_index": 1}}
{"type": "text.delta", "data": {"text": "Hello"}}
{"type": "thinking.delta", "data": {"text": "Let me analyze..."}}
{"type": "tool_use.start", "data": {"id": "tc_1", "tool": "read_file", "detail": "main.go"}}
{"type": "tool.progress", "data": {"id": "tc_1", "tool": "read_file", "status": "running"}}
{"type": "tool.result", "data": {"id": "tc_1", "tool": "read_file", "status": "completed"}}
{"type": "permission.request", "data": {"request_id": "abc-123", "tool": "write_file", "detail": "..."}}
{"type": "turn.complete", "data": {"stop_reason": "end_turn", "usage": {...}}}
{"type": "turn.error", "data": {"error": "context limit exceeded"}}
{"type": "usage.update", "data": {"input_tokens": 1500, "output_tokens": 200}}
```

Server sends ping frames every 10s; client responds with pong. Connection considered dead after 30s without pong.

**Authentication:** Random bearer token generated at startup, written to `~/.ycode/server.token`. For REST: `Authorization: Bearer <token>` header. For WebSocket: passed as `?token=<token>` query parameter on the upgrade request.

### NATS Integration

NATS serves as the distributed message bus, enabling remote clients to communicate with the server without direct WebSocket connections. This is useful for:

- Remote TUI connecting from a different machine
- Web clients behind a load balancer
- Multi-server deployments
- Decoupled client lifecycle (client can disconnect/reconnect without losing the session)

**NATS Subject Layout:**

```
ycode.sessions.{session_id}.events.>     Server publishes all events here
ycode.sessions.{session_id}.input        Client publishes commands here
ycode.rpc.{request_id}                   Request/reply for synchronous operations
```

**How it works:**

1. Server starts embedded NATS server (or connects to external NATS)
2. `NATSBus` bridges `bus.Bus` interface to NATS subjects
3. When `LocalService` publishes an event to the bus, `NATSBus` also publishes to `ycode.sessions.{id}.events.{type}`
4. Remote clients subscribe to session events via NATS
5. Remote clients send messages by publishing to `ycode.sessions.{id}.input`
6. Server subscribes to `ycode.sessions.*.input` and routes to `LocalService.SendMessage()`

**Synchronous operations** (config, session list, etc.) use NATS request/reply:
- Client publishes to `ycode.rpc.{uuid}` with reply subject
- Server handler responds on the reply subject
- Timeout after 5s

**Configuration:**
```json
{
  "nats": {
    "enabled": false,
    "url": "nats://localhost:4222",
    "embedded": true,
    "credentials": ""
  }
}
```

When `nats.embedded` is true, the server starts an embedded NATS server (using `github.com/nats-io/nats-server/v2/server`). When false, it connects to an external NATS instance.

### Client (`internal/client/`)

```go
type Client interface {
    service.Service
    Events(ctx context.Context, types ...bus.EventType) (<-chan bus.Event, error)
    Close() error
}
```

Three implementations:

1. **`InProcessClient`** — Holds direct references to `service.Service` and `bus.Bus`. Zero overhead. Used when TUI and server are in the same process (default `ycode` experience).

2. **`WSClient`** — Connects via WebSocket (`gorilla/websocket`). Sends commands as JSON messages, receives events from the same connection. For local web clients and local remote TUI. Handles reconnection with exponential backoff.

3. **`NATSClient`** — Connects via NATS. Subscribes to session events, publishes commands. For truly remote clients. Uses NATS request/reply for synchronous operations (config, session list). Can survive server restarts (NATS reconnects automatically).

### Event Flow

**Local WebSocket client sends a message:**
1. Client sends `{"type": "message.send", "data": {...}}` over WebSocket
2. Server's WebSocket handler calls `service.SendMessage()`
3. `LocalService` runs the agentic loop in a goroutine
4. Inside `conversation.Runtime.Turn()`, streaming deltas are published to the bus
5. Bus fans out events to all subscribers (including WebSocket write loops)
6. WebSocket write loop sends each event as a JSON message to the client
7. If NATS is enabled, `NATSBus` also publishes to NATS subjects

**Remote NATS client sends a message:**
1. Client publishes `{"type": "message.send", ...}` to `ycode.sessions.{id}.input`
2. Server's NATS input handler receives and calls `service.SendMessage()`
3. Same agentic loop runs, events published to bus
4. `NATSBus` publishes events to `ycode.sessions.{id}.events.{type}`
5. Remote client receives events via NATS subscription

**Permission prompts:**
1. Tool needs permission → service publishes `EventPermissionReq{requestID, toolName, detail}`
2. Client receives event (via WebSocket or NATS), shows prompt to user
3. User responds → client sends `{"type": "permission.respond", "data": {"request_id": "...", "allowed": true}}`
4. Service unblocks the tool goroutine waiting on a channel keyed by `requestID`

### TUI Refactoring

**Current state:** `TUIModel.app *App` → calls `app.RunTurnWithRecovery()` directly.

**Target state:** `TUIModel.client client.Client` → calls `client.SendMessage()` and listens on `client.Events()`.

Key changes to `internal/cli/tui.go`:
- Replace `app *App` field with `client client.Client`
- `startAgentTurn()`: call `client.SendMessage()` instead of `RunTurnWithRecovery()`, return a `tea.Cmd` that reads from the event channel
- Event-driven rendering: `bus.Event` messages are routed through BubbleTea's `Send()` as custom message types
- The multi-turn tool loop moves to the service layer — the TUI just renders events
- Permission: service publishes `EventPermissionReq`, TUI responds via `client.RespondPermission()`

**Backward compatibility:** `RunPrompt()` (headless one-shot mode) still works — it internally creates an `InProcessClient`, sends the message, and drains events to stdout.

### CLI Integration

**Default mode** (`ycode`): Creates `LocalService` + `InProcessClient` + TUI. Same as today but decoupled.

**Server mode** (`ycode serve api`): Starts HTTP/WebSocket server on port 58090 (distinct from observability on 58080). Optionally starts embedded NATS. Prints token and URL.

**Remote TUI via WebSocket** (`ycode --connect ws://localhost:58090`): Creates `WSClient`, launches TUI.

**Remote TUI via NATS** (`ycode --connect nats://localhost:4222`): Creates `NATSClient`, launches TUI.

**One-shot** (`ycode prompt "..."` or piped input): `InProcessClient` internally, prints text output, exits.

### Web Client

Minimal embedded SPA in `web/` directory:
- `index.html` — chat interface
- `app.js` — vanilla JS: `WebSocket` for conversation, `fetch` for REST
- `style.css` — minimal styling
- Markdown rendering via a small client-side library

The web client opens a WebSocket to `/api/sessions/{id}/ws` for the active session. REST calls handle session creation, listing, config. Embedded via `//go:embed` in `internal/web/embed.go`. Served at `/` by the HTTP server.

### Observability Integration

- HTTP middleware: OTEL trace spans per request, request/response metrics
- WebSocket metrics: active connection count, messages in/out per second
- NATS metrics: subscription count, messages published/received
- The existing OTEL collector at port 58080 receives telemetry from the API server automatically (same process, same OTEL SDK)

## Phased Implementation

### Phase 1: Event Bus

Create `internal/bus/` with `Bus` interface and `MemoryBus` implementation. Unit tests for fan-out, filtering, unsubscribe, slow consumer handling.

**Files:** `internal/bus/bus.go`, `internal/bus/memory.go`, `internal/bus/bus_test.go`

### Phase 2: Service Layer

Extract `Service` interface from `cli.App`. Create `LocalService` that wraps existing components. Move the agentic loop from `App.RunPrompt()` into `LocalService.SendMessage()` with bus event publishing.

**Files:** `internal/service/service.go`, `internal/service/types.go`, `internal/service/local.go`, `internal/service/local_test.go`
**Modified:** `internal/runtime/conversation/runtime.go` (add event publishing hooks in `Turn()`), `internal/cli/app.go` (delegate to service)

### Phase 3: HTTP + WebSocket Server

Build the HTTP server with REST endpoints for CRUD and WebSocket endpoint for conversation. Bearer token auth. Integration tests with `httptest.Server`.

**Files:** `internal/server/server.go`, `internal/server/routes.go`, `internal/server/handler_rest.go`, `internal/server/handler_ws.go`, `internal/server/middleware.go`, `internal/server/server_test.go`
**Modified:** `cmd/ycode/` (add `serve api` command)

### Phase 4: Go Clients

Create `InProcessClient` and `WSClient` implementations. Tests against `httptest.Server`.

**Files:** `internal/client/client.go`, `internal/client/inprocess.go`, `internal/client/ws.go`, `internal/client/ws_test.go`

### Phase 5: TUI Refactor

Rewrite TUI to use `client.Client` instead of `*App`. Wire `InProcessClient` for default mode. Verify identical user experience.

**Modified:** `internal/cli/tui.go`, `internal/cli/app.go`

### Phase 6: NATS Integration

Add `NATSBus` implementation and `NATSClient`. Optional embedded NATS server. Enable remote client connections.

**Files:** `internal/bus/nats.go`, `internal/bus/nats_test.go`, `internal/client/nats.go`, `internal/client/nats_test.go`
**Modified:** `internal/server/server.go` (NATS input subscription), config (NATS settings)

### Phase 7: Remote TUI

Add `--connect` flag. TUI connects to running server via `WSClient` or `NATSClient`.

**Modified:** `cmd/ycode/main.go`, `internal/cli/app.go`

### Phase 8: Web Client

Build minimal embedded web UI using WebSocket for conversation.

**Files:** `web/index.html`, `web/app.js`, `web/style.css`, `internal/web/embed.go`, `internal/web/handler.go`

### Phase 9: Observability

Wire OTEL middleware into HTTP server. WebSocket and NATS connection metrics. Dashboard integration.

**Modified:** `internal/server/middleware.go`, `internal/server/server.go`

## Verification

Each phase has a concrete test:

1. **Bus:** Unit test — publish event, subscriber receives it; filtered subscribe; slow consumer
2. **Service:** Unit test — `SendMessage` publishes events to bus
3. **Server:** Integration test — WebSocket client sends message, receives streaming events
4. **Client:** Integration test — `WSClient` round-trip through `httptest.Server`
5. **TUI refactor:** Manual test — interactive TUI works identically
6. **NATS:** Integration test — `NATSClient` sends message, receives events via NATS
7. **Remote TUI:** Manual test — `ycode serve api` in one terminal, `ycode --connect` in another
8. **Web:** Manual test — open browser, send message, see streaming response
9. **Observability:** Check Prometheus dashboard for HTTP/WebSocket/NATS metrics

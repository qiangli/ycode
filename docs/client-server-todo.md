# Client/Server Architecture — Implementation Checklist

Tracks progress for the plan in [client-server-architecture.md](./client-server-architecture.md).

---

## Phase 1: Event Bus

- [x] Define `Bus` interface and `EventType` constants in `internal/bus/bus.go`
- [x] Define `Event` struct with ID, Type, SessionID, Timestamp, Data fields
- [x] Implement `MemoryBus` in `internal/bus/memory.go` with fan-out via goroutines + buffered channels
- [x] Implement subscriber filtering by event type
- [x] Implement unsubscribe (remove subscriber, close channel)
- [x] Handle slow consumers (non-blocking send, drop or disconnect)
- [x] Add monotonic event ID for reconnection replay
- [x] Add ring buffer for recent events (replay on reconnect)
- [x] Write unit tests (`internal/bus/bus_test.go`)

## Phase 2: Service Layer

- [x] Define `Service` interface in `internal/service/service.go`
- [x] Define `AppBackend` interface in `internal/service/app.go` (breaks import cycle with cli)
- [x] Define DTO types (`SessionInfo`, `StatusInfo`, `MessageInput`) — `MessageInput` in `bus` package for cycle-free sharing
- [x] Implement `LocalService` in `internal/service/local.go`
- [x] Extract agentic loop from `cli.App.RunPrompt()` into `LocalService.SendMessage()`
- [x] Publish `EventTurnStart` at turn start
- [x] Publish `EventTextDelta` / `EventThinkingDelta` as streaming chunks arrive from `conversation.Runtime.Turn()`
- [x] Publish `EventToolUseStart` during tool execution
- [x] Publish `EventToolProgress` with per-tool status (queued/running/completed/failed)
- [x] Publish `EventToolResult` after tool completion
- [x] Publish `EventTurnComplete` or `EventTurnError` at turn end
- [x] Publish `EventUsageUpdate` with token counts after each turn
- [x] Implement `RespondPermission()` to unblock waiting tool goroutine
- [x] Implement `CancelTurn()` using context cancellation
- [x] Modify `conversation.Runtime.Turn()` to accept an optional event callback for streaming deltas
- [x] Serialize per-session writes with `sync.Map` keyed by session ID
- [x] Write unit tests (`internal/service/local_test.go`)

## Phase 3: HTTP + WebSocket Server

- [x] Add `github.com/gorilla/websocket` dependency
- [x] Create `Server` struct in `internal/server/server.go` wrapping `http.Server` + `service.Service`
- [x] Set up `net/http.ServeMux` with Go 1.22+ method+path patterns
- [x] Implement `GET /api/health` handler
- [x] Implement `GET /api/config` handler
- [x] Implement `PUT /api/config/model` handler
- [x] Implement `GET /api/sessions` handler (list)
- [x] Implement `POST /api/sessions` handler (create)
- [x] Implement `GET /api/sessions/{id}` handler (get)
- [x] Implement `GET /api/sessions/{id}/messages` handler (history)
- [x] Implement `POST /api/commands/{name}` handler
- [x] Implement `GET /api/status` handler
- [x] Implement WebSocket upgrade at `GET /api/sessions/{id}/ws`
- [x] WebSocket read loop: parse client messages (`message.send`, `permission.respond`, `turn.cancel`)
- [x] WebSocket write loop: subscribe to bus, send events as JSON frames
- [x] WebSocket: ping/pong every 10s, dead connection after 60s
- [x] WebSocket: reconnection replay from ring buffer via `last_event_id` in upgrade query
- [x] Implement bearer token auth middleware (generate token at startup, write to `~/.ycode/server.token`)
- [x] WebSocket auth: validate `?token=` query parameter on upgrade
- [x] Implement CORS middleware
- [x] Implement request logging middleware
- [x] Serve embedded web UI at `/`
- [x] `ycode serve` starts ALL services by default (otel + API + NATS)
- [x] `--no-api` flag to disable API server
- [x] `--no-nats` flag to disable NATS server
- [x] Write integration tests with `httptest.Server` (`internal/server/server_test.go`)

## Phase 4: Go Clients

- [x] Define `Client` interface in `internal/client/client.go` (extends `service.Service` + `Events()` + `Close()`)
- [x] Implement `InProcessClient` in `internal/client/inprocess.go` (direct delegation to service + bus)
- [x] Implement `WSClient` in `internal/client/ws.go` using `gorilla/websocket`
- [x] `WSClient`: open WebSocket to `/api/sessions/{id}/ws`
- [x] `WSClient`: send `message.send` / `permission.respond` / `turn.cancel` JSON messages
- [x] `WSClient`: receive events from WebSocket read loop, fan out to `Events()` channel
- [x] `WSClient`: REST calls for session CRUD, config, commands (via HTTP)
- [x] `WSClient`: handle reconnection with exponential backoff
- [x] `WSClient`: pass bearer token as `?token=` on WebSocket upgrade and `Authorization` header on REST
- [x] Write integration tests (`internal/client/ws_test.go`)

## Phase 5: TUI Refactor

- [x] Add `agentClient` interface in `cli/tui.go` (avoids import cycle with `client` package)
- [x] Add `cl agentClient` field to `TUIModel` (optional, alongside existing `*App`)
- [x] Add `startAgentTurnViaClient()` — sends message through service layer, forwards bus events to TUI
- [x] Add `busEventMsg` type for routing bus events through BubbleTea's message system
- [x] Handle `EventTextDelta` — append text to viewport
- [x] Handle `EventThinkingDelta` — render thinking content
- [x] Handle `EventToolUseStart` / `EventToolProgress` / `EventToolResult` — update tool progress display
- [x] Handle `EventTurnComplete` — finalize turn, update status bar
- [x] Handle `EventTurnError` — display error
- [x] Handle `EventPermissionReq` — show confirmation dialog
- [x] Handle `EventUsageUpdate` — update token counters
- [x] Add `RunInteractiveWithClient()` to wire an `agentClient` into the TUI
- [x] Existing TUI continues to work via direct `*App` path (backward compatible)

## Phase 6: NATS Integration

- [x] Add `github.com/nats-io/nats.go` dependency
- [x] Add `github.com/nats-io/nats-server/v2` dependency (for embedded server)
- [x] Implement `NATSBus` in `internal/bus/nats.go` implementing `bus.Bus`
- [x] Map event types to NATS subjects: `ycode.sessions.{session_id}.events.{type}`
- [x] `NATSBus.Publish()`: serialize Event to JSON, publish to NATS subject
- [x] `NATSBus.Subscribe()`: NATS subscription with Go channel delivery + filter mapping
- [x] Support wildcard subscription: `ycode.sessions.{id}.events.>`
- [x] Add NATS input subscription on server: `ycode.sessions.*.input` → route to `service.SendMessage()`
- [x] Add embedded NATS server option in server startup (`internal/server/nats.go`)
- [x] Bridge local bus events to NATS for remote clients
- [x] `ycode serve` starts NATS by default; `--no-nats` to disable
- [x] Implement NATS request/reply handler for synchronous operations (status, config, sessions)
- [x] Add NATS configuration to settings.json schema (`config.NATSConfig`)
- [x] Implement `NATSClient` in `internal/client/nats.go`
- [x] `NATSClient`: publish commands to `ycode.sessions.{id}.input`
- [x] `NATSClient`: subscribe to `ycode.sessions.{id}.events.>` for events
- [x] `NATSClient`: NATS request/reply for synchronous service calls
- [x] `NATSClient`: automatic reconnection (built into nats.go)
- [x] Write unit tests (`internal/bus/nats_test.go`)
- [x] Write integration tests (`internal/client/nats_test.go`)
- [x] NATS test client (`cmd/nats-test-client/main.go`)

## Phase 7: Remote TUI

- [x] Add `--connect` flag to root command in `cmd/ycode/main.go`
- [x] Parse scheme to select client: `ws://`/`http://` → `WSClient`, `nats://` → `NATSClient`
- [x] Read bearer token from `~/.ycode/server.token`
- [x] WSClient connects, verifies server status, reports session info
- [x] Stub for full TUI: awaits wiring `RunInteractiveWithClient()` with remote WSClient/NATSClient

## Phase 8: Web Client

- [x] Create `web/index.html` with chat UI layout
- [x] Create `web/app.js` — vanilla JS: `WebSocket` for conversation, `fetch` for REST
- [x] Create `web/style.css` — dark theme, monospace, tool progress indicators
- [x] Implement session auto-detection via REST `/api/status`
- [x] Implement WebSocket connection to `/api/sessions/{id}/ws`
- [x] Send messages via WebSocket `message.send` frame
- [x] Render `text.delta` events incrementally with streaming cursor
- [x] Show tool execution progress from `tool_use.start` / `tool.progress` / `tool.result`
- [x] Handle `permission.request` events — show Allow/Deny buttons, send `permission.respond`
- [x] Handle `turn.complete` / `turn.error` — finalize turn display
- [x] Display `thinking.delta` in italic block
- [x] Token usage display in header
- [x] Simple markdown rendering (code blocks, inline code, bold)
- [x] Auto-reconnect WebSocket on disconnect (2s delay)
- [x] Create `internal/web/embed.go` with `//go:embed` for static assets
- [x] Mount web handler at `/` in server routes

## Phase 9: Observability

- [x] Add OTEL HTTP middleware to server (`internal/server/otel.go`) — trace span per request
- [x] Add HTTP request metrics (count via `Int64Counter`, latency via `Float64Histogram`)
- [x] Add WebSocket connection metrics (active count via `Int64ObservableGauge`)
- [x] Add NATS metrics counter (`Int64Counter` for messages)
- [x] `SetOTEL()` method to configure instrumentation before server start
- [x] Middleware wired into HTTP handler chain: `otelMiddleware(corsMiddleware(requestLogger(mux)))`
- [x] WS connect/disconnect tracked via `trackWSConnect()`/`trackWSDisconnect()`
- [x] Write OTEL middleware tests (`internal/server/otel_test.go`)

# NATS-Based Messaging Hub with Platform Bridges

## Context

ycode needs Slack-like messaging capabilities. Rather than introducing a foreign protocol (Matrix/Dendrite), we build directly on the **NATS messaging system already embedded in ycode**. The hub bridges bidirectionally to Telegram, Discord, and WeChat.

Design follows **OpenClaw's channel/adapter pattern** (from `priorart/openclaw/`): channels as pluggable adapters with capabilities, account-scoped state, binding-based routing, and lifecycle management — adapted to Go idioms and the existing ycode component model.

**All dependencies must use permissive licenses (MIT, Apache-2.0, BSD).**

---

## Architecture Overview

```
┌──────────────────────────────────────────────────┐
│  ycode serve (single binary)                     │
│                                                  │
│  ┌──────────────────────────────────────────┐    │
│  │  Embedded NATS (existing, port 4222)     │    │
│  │  ycode.chat.rooms.{id}.messages          │    │
│  │  ycode.chat.channels.{id}.inbound        │    │
│  │  ycode.chat.channels.{id}.outbound       │    │
│  │  ycode.chat.channels.{id}.status         │    │
│  └──────────┬───────────────────────────────┘    │
│             │                                    │
│  ┌──────────▼───────────────────────────────┐    │
│  │  Chat Hub (internal/chat/hub.go)         │    │
│  │  Router · Store · Channel Registry       │    │
│  └──┬──────────┬──────────┬──────────┬──────┘    │
│     │          │          │          │            │
│  ┌──▼──┐  ┌───▼───┐  ┌───▼───┐  ┌───▼───┐       │
│  │ Web │  │Telegram│  │Discord│  │WeChat │       │
│  │ WS  │  │ gotd   │  │discgo │  │ WeCom │       │
│  └─────┘  └───────┘  └───────┘  └───────┘       │
│                                                  │
│  Proxy: /chat/ → Hub HTTPHandler                 │
└──────────────────────────────────────────────────┘
```

---

## NATS Subject Design

Extends existing `ycode.sessions.*` with parallel `ycode.chat.*` namespace:

```
ycode.chat.rooms.{room_id}.messages       — new messages in a room
ycode.chat.rooms.{room_id}.presence       — typing/join/leave
ycode.chat.channels.{channel_id}.inbound  — raw inbound from adapters
ycode.chat.channels.{channel_id}.outbound — formatted outbound to adapters
ycode.chat.channels.{channel_id}.status   — adapter health events
ycode.chat.rpc.>                          — request/reply (list rooms, history)
```

---

## Package Layout

```
internal/chat/
    hub.go              — Hub component (observability.Component)
    message.go          — Unified message types
    router.go           — Room/Binding routing
    store.go            — SQLite persistence
    channel/
        channel.go      — Channel interface, ChannelID, Capabilities
    adapters/
        web.go          — Web channel (WebSocket)
        telegram.go     — Telegram (gotd/td, MIT)
        discord.go      — Discord (discordgo, BSD-3)
        wechat.go       — WeCom enterprise API (pure HTTP)
        mock.go         — Mock channel for tests
    web/
        embed.go        — //go:embed static files
        static/
            index.html
            style.css
            chat.js
```

---

## Core Types

### Channel Interface (`internal/chat/channel/channel.go`)

Adapted from OpenClaw's ChannelPlugin — distilled to essentials:

```go
type Channel interface {
    ID() ChannelID
    Capabilities() Capabilities
    Start(ctx context.Context, accounts []AccountConfig, inbound chan<- InboundMessage) error
    Stop(ctx context.Context) error
    Healthy() bool
    Send(ctx context.Context, target OutboundTarget, msg OutboundMessage) error
}

type Capabilities struct {
    Threads, Reactions, EditMessage, Media, Markdown bool
    MaxTextLen int
}
```

### Unified Message Model (`internal/chat/message.go`)

```go
type Message struct {
    ID, RoomID string; Sender Sender; Timestamp time.Time
    Content MessageContent; ReplyTo, ThreadID string
    Origin  MessageOrigin
}
type Sender struct { ID, DisplayName string; ChannelID ChannelID; PlatformID string }
type MessageContent struct { Text, HTML string; Attachments []Attachment }
type MessageOrigin struct { ChannelID ChannelID; AccountID, PlatformID string }
```

### Routing (`internal/chat/router.go`)

```go
type Binding struct { ChannelID ChannelID; AccountID, ChatID string }
type Room struct { ID, Name string; Bindings []Binding }
```

- **Inbound**: lookup room by `(channel_id, account_id, chat_id)` → auto-create if none
- **Outbound**: fan out to all room bindings except the origin binding

### Hub Component (`internal/chat/hub.go`)

Implements `observability.Component`:
- `Name() → "chat"`
- `Start()`: open NATS subscriptions, start all enabled channel adapters, run inbound router goroutine
- `Stop()`: stop adapters, drain NATS subs
- `HTTPHandler()`: serves web UI + REST/WebSocket API

---

## Hub HTTP API (served at `/chat/`)

```
GET    /api/rooms                    — list rooms
POST   /api/rooms                    — create room
GET    /api/rooms/{id}/messages      — history (paginated)
POST   /api/rooms/{id}/messages      — send message
GET    /api/rooms/{id}/ws            — WebSocket for live messages
GET    /api/channels                 — channel statuses
GET    /api/health                   — hub health
```

Non-API paths serve the embedded web UI static files.

---

## Web UI (`internal/chat/web/static/`)

Vanilla HTML/CSS/JS SPA — same pattern as existing `web/`:
- Room list sidebar
- Message timeline with auto-scroll
- Input bar (text + file upload)
- WebSocket for real-time updates, REST for history
- Connection status indicator

---

## Per-Channel Adapters

### Telegram (`internal/chat/adapters/telegram.go`)
- **Library**: `github.com/gotd/td` (MIT) — full MTProto 2.0
- **Config**: `bot_token` (or `phone` + `api_id` + `api_hash` for user-account puppeting)
- **Inbound**: long-poll or MTProto updates → convert to `InboundMessage`
- **Outbound**: `messages.SendMessage` / `messages.SendMedia`
- **Capabilities**: `{Threads: true, Reactions: true, Media: true, Markdown: true, MaxTextLen: 4096}`

### Discord (`internal/chat/adapters/discord.go`)
- **Library**: `github.com/bwmarrin/discordgo` (BSD-3-Clause)
- **Config**: `bot_token`, optional `guild_id` filter
- **Inbound**: gateway WebSocket `MessageCreate` handler → `InboundMessage`
- **Outbound**: `ChannelMessageSend` / `ChannelMessageSendComplex`
- **Capabilities**: `{Threads: true, Reactions: true, EditMessage: true, Media: true, Markdown: true, MaxTextLen: 2000}`

### WeChat (`internal/chat/adapters/wechat.go`)
- **Approach**: WeCom (Enterprise WeChat) REST API — official, stable, documented
- **Config**: `corp_id`, `agent_id`, `secret`
- **Inbound**: XML callback endpoint (hub must be reachable or use tunnel)
- **Outbound**: POST to `qyapi.weixin.qq.com/cgi-bin/message/send`
- **Capabilities**: `{Media: true, Markdown: true (limited), MaxTextLen: 2048}`
- **Note**: Personal WeChat Web protocol is fragile and ToS-gray — defer to later if needed

### Web (`internal/chat/adapters/web.go`)
- Built-in, uses `gorilla/websocket` (already in deps)
- Simplest adapter — WebSocket ↔ InboundMessage/OutboundMessage

---

## Persistence (SQLite)

Add migration to existing `internal/storage/sqlite/`:

```sql
CREATE TABLE chat_rooms (
    id TEXT PRIMARY KEY, name TEXT, created_at TEXT, updated_at TEXT
);
CREATE TABLE chat_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    room_id TEXT REFERENCES chat_rooms(id) ON DELETE CASCADE,
    channel_id TEXT, account_id TEXT DEFAULT 'default', chat_id TEXT,
    UNIQUE(channel_id, account_id, chat_id)
);
CREATE TABLE chat_messages (
    id TEXT PRIMARY KEY, room_id TEXT REFERENCES chat_rooms(id),
    sender_id TEXT, sender_name TEXT, channel_id TEXT, platform_id TEXT,
    content TEXT, reply_to TEXT, thread_id TEXT, timestamp TEXT
);
CREATE TABLE chat_users (
    id TEXT PRIMARY KEY, display_name TEXT,
    channel_id TEXT, platform_id TEXT,
    UNIQUE(channel_id, platform_id)
);
```

Store wrapper in `internal/chat/store.go`.

---

## Configuration

Add to `internal/runtime/config/config.go`:

```go
type ChatConfig struct {
    Enabled  bool                     `json:"enabled"`
    Channels map[string]ChannelConfig `json:"channels,omitempty"`
}
type ChannelConfig struct {
    Enabled  bool           `json:"enabled"`
    Accounts []AccountEntry `json:"accounts,omitempty"`
}
type AccountEntry struct {
    ID      string            `json:"id"`
    Enabled bool              `json:"enabled"`
    Config  map[string]string `json:"config"`
}
```

Add `Chat *ChatConfig` to root Config struct. Example:
```json
{
  "chat": {
    "enabled": true,
    "channels": {
      "telegram": {
        "enabled": true,
        "accounts": [{"id": "default", "enabled": true, "config": {"bot_token": "123:ABC"}}]
      },
      "discord": {
        "enabled": true,
        "accounts": [{"id": "default", "enabled": true, "config": {"bot_token": "MTk..."}}]
      }
    }
  }
}
```

---

## Wiring into ycode serve

In `cmd/ycode/serve.go` `runAllServices()`, after the API/NATS stack:

```go
if cfg.Chat != nil && cfg.Chat.Enabled {
    chatHub := chat.NewHub(api.natsSrv.Conn(), cfg.Chat, filepath.Join(dataDir, "chat"))
    if err := mgr.AddLateComponent(ctx, chatHub); err != nil {
        slog.Warn("chat hub not available", "error", err)
    } else {
        fmt.Printf("Chat at             http://127.0.0.1:%d/chat/\n", port)
    }
}
```

Add `"chat": "/chat/"` to `componentPathMap` in `internal/observability/stack.go`.

---

## Phased Implementation

### Phase 1: Core + Web channel (first usable vertical)
1. `internal/chat/channel/channel.go` — Channel interface, types
2. `internal/chat/message.go` — unified message model
3. `internal/chat/store.go` + SQLite migration — persistence
4. `internal/chat/router.go` — room/binding routing
5. `internal/chat/hub.go` — Hub component with NATS pub/sub
6. `internal/chat/adapters/web.go` — WebSocket channel
7. `internal/chat/web/` — chat web UI (HTML/CSS/JS)
8. Config additions, serve.go wiring, componentPathMap entry
9. **Verify**: browser at `/chat/`, create room, send/receive messages

### Phase 2: Telegram bridge
1. Add `github.com/gotd/td` to go.mod
2. `internal/chat/adapters/telegram.go`
3. **Verify**: Telegram ↔ hub ↔ web UI bidirectional

### Phase 3: Discord bridge
1. Add `github.com/bwmarrin/discordgo` to go.mod
2. `internal/chat/adapters/discord.go`
3. **Verify**: Discord ↔ hub ↔ web UI bidirectional

### Phase 4: WeChat (WeCom) bridge
1. `internal/chat/adapters/wechat.go` — pure HTTP, no external dep
2. Webhook ingress handling
3. **Verify**: WeCom ↔ hub ↔ web UI bidirectional

### Phase 5: Polish
- Room management UI, message history pagination
- Presence/typing indicators, media relay
- Health dashboard integration

---

## Testing

- **Unit**: `router_test.go`, `store_test.go`, `message_test.go`, per-adapter `_test.go` with mock HTTP
- **Integration** (`internal/integration/chat_test.go`): embedded NATS + hub + web adapter end-to-end
- **Mock channel** (`adapters/mock.go`): implements Channel for testing bridge fan-out
- All tests pure Go, no external services required

---

## Files to Modify (existing)

| File | Change |
|------|--------|
| `go.mod` | Add gotd/td, discordgo deps |
| `internal/runtime/config/config.go` | Add ChatConfig, ChannelConfig types |
| `internal/observability/stack.go` | Add `"chat": "/chat/"` to componentPathMap |
| `cmd/ycode/serve.go` | Instantiate and wire Hub component |
| `internal/storage/sqlite/` | Add chat tables migration |

## New Dependencies

| Library | License | Purpose |
|---------|---------|---------|
| `github.com/gotd/td` | MIT | Telegram MTProto 2.0 |
| `github.com/bwmarrin/discordgo` | BSD-3-Clause | Discord gateway + REST |
| (none for WeChat) | — | WeCom uses pure HTTP |

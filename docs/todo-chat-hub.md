# Chat Hub — Implementation Checklist

Plan: [plan-chat-hub.md](./plan-chat-hub.md)

## Phase 1: Core + Web Channel

- [x] Create `internal/chat/channel/channel.go` — Channel interface, ChannelID, Capabilities, AccountConfig, message types
- [x] Create `internal/chat/message.go` — Hub message model (Message, Sender, MessageOrigin)
- [x] Create `internal/chat/store.go` — SQLite persistence (rooms, bindings, messages, users)
- [x] Create `internal/chat/router.go` — Room/binding routing with auto-create and fan-out
- [x] Create `internal/chat/hub.go` — Hub component (observability.Component) with NATS pub/sub, REST API
- [x] Create `internal/chat/ws.go` — WebSocket hub for real-time message delivery
- [x] Create `internal/chat/webhandler.go` — Wires embedded static file handler
- [x] Create `internal/chat/adapters/web.go` — Built-in web channel adapter
- [x] Create `internal/chat/web/embed.go` — go:embed for static assets
- [x] Create `internal/chat/web/static/index.html` — SPA layout
- [x] Create `internal/chat/web/static/chat.js` — Vanilla JS Matrix-free client
- [x] Create `internal/chat/web/static/style.css` — Dark theme styles
- [x] Add `ChatConfig` to `internal/runtime/config/config.go`
- [x] Add `"chat": "/chat/"` to `componentPathMap` in `internal/observability/stack.go`
- [x] Create `cmd/ycode/serve_chat.go` — Hub builder with channel registration
- [x] Wire Hub into `cmd/ycode/serve.go` `runAllServices()`

## Phase 2: Telegram Bridge

- [x] Create `internal/chat/adapters/telegram.go` — Bot API long-polling, sendMessage

## Phase 3: Discord Bridge

- [x] Add `github.com/bwmarrin/discordgo` (BSD-3) to go.mod
- [x] Create `internal/chat/adapters/discord.go` — Gateway WebSocket, message handler

## Phase 4: WeChat (WeCom) Bridge

- [x] Create `internal/chat/adapters/wechat.go` — WeCom REST API, callback server, token refresh

## Phase 5: Testing

- [x] Create `internal/chat/adapters/mock.go` — Test double channel
- [x] Create `internal/chat/adapters/util.go` — Shared helpers
- [x] Create `internal/chat/store_test.go` — Room/binding/message/user CRUD
- [x] Create `internal/chat/router_test.go` — Auto-create, fan-out
- [x] Create `internal/chat/message_test.go` — JSON serialization roundtrip
- [x] `go build ./cmd/ycode/` — clean
- [x] `go vet` — clean
- [x] `go test -short -race ./internal/chat/...` — all pass
- [x] `go mod tidy` — clean

## Phase 6: Polish (future)

- [ ] Room management UI (rename, add bindings between channels)
- [ ] Message history pagination in web UI
- [ ] Presence/typing indicators
- [ ] Media upload and relay across channels
- [ ] Health dashboard integration
- [ ] Upgrade Telegram adapter to use `gotd/td` (MIT) for full MTProto 2.0
- [ ] Integration tests with embedded NATS (`internal/integration/chat_test.go`)

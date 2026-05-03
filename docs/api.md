# ycode Server API

The ycode server exposes a REST + WebSocket API for building thin clients in any language.

**Default:** `http://127.0.0.1:58080`
**API prefix:** `/ycode/` (the API is mounted on the observability proxy)

All endpoints below are relative to the API prefix: `http://127.0.0.1:58080/ycode/`

## Server Discovery

The server writes discovery files for client use:
- `~/.agents/ycode/serve.pid` — PID of the running server process
- `~/.agents/ycode/serve.port` — Port the server is listening on
- `~/.agents/ycode/server.token` — Authentication token

## Authentication

Bearer token from `~/.agents/ycode/server.token`:
```
Authorization: Bearer <token>
```

The `/api/health` endpoint does not require authentication.

## REST Endpoints

### Health Check
```
GET /api/health
```
Returns `{"status":"ok"}`. Use this to detect a running server.

Full URL: `http://127.0.0.1:58080/ycode/api/health`

### Server Status
```
GET /api/status
```
Returns active session ID, model, and server metadata.
```json
{"session_id": "abc123", "model": "claude-sonnet-4-20250514", "version": "1.0.0"}
```

### Sessions

```
POST /api/sessions         — Create a new session
GET  /api/sessions         — List all sessions
GET  /api/sessions/{id}    — Get session info
GET  /api/sessions/{id}/messages — Get conversation history
```

### Configuration
```
GET /api/config            — Get current configuration
PUT /api/config/model      — Switch model (body: {"model": "..."})
```

### Models
```
GET /api/models            — List available models
```

### Commands
```
POST /api/commands/{name}  — Execute a slash command (body: {"args": "..."})
```

## WebSocket Protocol

### Connection
```
GET /api/sessions/{id}/ws
```

Upgrades to WebSocket. Optionally pass `?last_event_id=N` for event replay on reconnect.

### Client → Server Messages

#### Send a prompt
```json
{"type": "message.send", "data": {"text": "your prompt here", "files": []}}
```

#### Cancel in-flight turn
```json
{"type": "turn.cancel", "data": {}}
```

#### Respond to permission request
```json
{"type": "permission.respond", "data": {"request_id": "...", "allowed": true}}
```

### Server → Client Events

#### Text streaming
```json
{"type": "text.delta", "data": {"text": "partial response..."}}
```

#### Thinking (extended thinking / chain-of-thought)
```json
{"type": "thinking.delta", "data": {"text": "reasoning..."}}
```

#### Tool use started
```json
{"type": "tool_use.start", "data": {"id": "call_123", "tool": "Bash", "detail": "git status"}}
```

#### Tool progress
```json
{"type": "tool.progress", "data": {"id": "call_123", "output": "partial output..."}}
```

#### Tool result
```json
{"type": "tool.result", "data": {"id": "call_123", "output": "full result", "error": ""}}
```

#### Permission request
```json
{"type": "permission.request", "data": {"request_id": "perm_456", "tool": "Bash", "detail": "rm -rf /tmp/old"}}
```
Respond with `permission.respond` message.

#### Turn complete
```json
{"type": "turn.complete", "data": {"stop_reason": "end_turn", "usage": {"input_tokens": 1200, "output_tokens": 450}}}
```

#### Turn error
```json
{"type": "turn.error", "data": {"error": "rate limit exceeded"}}
```

#### Usage update
```json
{"type": "usage.update", "data": {"input_tokens": 1200, "output_tokens": 450, "cost_usd": 0.012}}
```

## Detecting a Running Server

To detect a running ycode server programmatically:

1. Read port from `~/.agents/ycode/serve.port` (fall back to 58080)
2. Check PID file exists: `~/.agents/ycode/serve.pid`
3. Verify process is alive (signal 0 or equivalent)
4. HTTP GET `http://127.0.0.1:{port}/ycode/api/health` with short timeout

If not running, start one:
```bash
ycode serve --detach
```

## Example Flow

```
1. GET  /ycode/api/health              → verify server
2. GET  /ycode/api/status              → get session_id
3. WS   /ycode/api/sessions/{id}/ws   → upgrade to WebSocket
4. Send: {"type":"message.send","data":{"text":"hello"}}
5. Recv: {"type":"text.delta","data":{"text":"Hi"}}
6. Recv: {"type":"text.delta","data":{"text":"! How can"}}
7. Recv: {"type":"text.delta","data":{"text":" I help?"}}
8. Recv: {"type":"turn.complete","data":{"stop_reason":"end_turn",...}}
```

See `examples/node-client/` and `examples/python-client/` for working implementations.

# ycode wire API for third-party hosts

This document is the contract between `ycode serve` and a third-party
application (e.g. classgo) that integrates over the wire — never as a
linked Go library. Everything in here is reachable via plain HTTP +
WebSocket and survives across `ycode serve` restarts.

For ycode-as-library use (`pkg/ycode.Agent`), see `pkg/ycode/doc.go`. For
the original chat / session API, see [api.md](./api.md). This file
focuses on the surface added for embedding ycode in another product:
auth + identity, stateless extract / embed endpoints, per-session
overrides, and the operator flags that make the server safe to share.

## Discovery

`ycode serve` writes `~/.agents/ycode/manifest.json` (schema v3). Read
this on startup instead of hard-coding ports or header names. The
relevant new block:

```json
{
  "schemaVersion": "3",
  "endpoints": {
    "proxy":    "http://127.0.0.1:58080",
    "api":      "http://127.0.0.1:58080/ycode/",
    "...":      "..."
  },
  "auth": {
    "scheme":          "bearer",
    "header":          "Authorization",
    "wsQueryParam":    "token",
    "tokenFile":       "/Users/<you>/.agents/ycode/server.token",
    "actorHeaders":    ["X-Actor-User", "X-Actor-Email", "X-Actor-Roles"],
    "actorExtraPrefix": "X-Actor-Extra-",
    "enabled":         true
  }
}
```

`auth.enabled` is `false` when the server was started with `--no-auth`.

## Authentication and identity

Two layers, evaluated in order on every REST request and every WebSocket
upgrade:

1. **Service-to-service auth (G-D).** The server validates a Bearer
   token against the random token written to `~/.agents/ycode/server.token`
   on each `ycode serve` start. WebSocket clients that cannot set
   headers may pass the token as `?token=<token>` instead.

   `Token == ""` (set by `--no-auth`) puts the middleware in permissive
   mode — useful for local dev and the single-tenant TUI. Production
   deployments **must not** disable auth.

2. **End-user identity (G-C).** When the bearer check passes, the
   middleware decodes the `X-Actor-*` headers into a
   `pkg/ycode/actor.User` and stamps it onto the request `Context`.
   Custom tools registered against the agent retrieve it via
   `actor.UserFromContext(ctx)` or the `actor.HasRole(ctx, role)`
   convenience.

| Header | Maps to | Notes |
|---|---|---|
| `X-Actor-User` | `actor.User.ID` | Required to populate identity. Without it, no `actor.User` is stamped. |
| `X-Actor-Email` | `actor.User.Email` | Optional. |
| `X-Actor-Roles` | `actor.User.Roles` | Comma-separated; whitespace trimmed. |
| `X-Actor-Extra-<key>` | `actor.User.Extra[lowercase-key]` | Zero or more host-specific attributes. |

**Identity is only decoded after the bearer check passes**, so an
unauthenticated caller cannot spoof an actor.

## Stateless one-shot endpoints

These endpoints do **not** drive the agentic conversation loop, do not
touch session state, and do not publish bus events. They are pure HTTP
in / HTTP out.

### `POST /api/extract` — JSON-constrained one-shot LLM call

```
POST /api/extract
Authorization: Bearer <token>
X-Work-Dir: /path/to/tenant-project        (or ?work_dir= or body field)
X-Actor-User: parent_47                     (optional)
Content-Type: application/json

{
  "model":       "claude-sonnet-4-6",       // optional; defaults to cfg.Model
  "max_tokens":  4096,                       // optional; defaults to cfg.MaxTokens or 4096
  "system":      "You are a tutor.",         // optional system prompt
  "schema":      { "type": "object", "...": "..." },  // optional JSON Schema
  "schema_name": "signoff",                  // optional label (OpenAI)
  "prompt":      "Summarize today's lesson."
}

200 OK
Content-Type: application/json
<the raw JSON the model produced — already valid JSON, returned verbatim>

400  prompt missing, work_dir missing, body unparseable
401  bearer auth failed
500  model error
503  no LLM provider configured
```

`schema` is forwarded as `response_format.json_schema` for
OpenAI-compatible providers and translated into a forced `tool_use`
shim for Anthropic. When `schema` is absent the call falls back to
`json_object` mode (free-form JSON object).

### `POST /api/embed` — single embedding

```
POST /api/embed
{ "text": "..." }

200 OK
{ "vector": [0.12, -0.04, ...], "dimensions": 384 }
```

### `POST /api/embed/batch` — batched embeddings

```
POST /api/embed/batch
{ "texts": ["a", "b", "c"] }

200 OK
{ "vectors": [[...], [...], [...]], "dimensions": 384 }
```

### `GET /api/embed/dimensions` — vector dim

```
GET /api/embed/dimensions
200 OK
{ "dimensions": 384 }
```

The embedding provider is determined by the standard precedence ladder:
`YCODE_EMBEDDING_API=true` + `OPENAI_API_KEY` → Ollama with
`YCODE_OLLAMA_EMBEDDING=true` → TF-IDF (always available, free).

## Per-session overrides

`POST /api/sessions` accepts an optional `session_options` body field that
is stored on the session and echoed back in `SessionInfo`:

```
POST /api/sessions
{
  "work_dir": "/tmp/tenant-project",
  "session_options": {
    "model":            "claude-haiku-4-5-20251001",  // G-G
    "tools_allowlist":  ["read_file", "grep_search"],   // G-E (wire shape)
    "tools_blocklist":  ["bash"],                       // G-E (wire shape)
    "persona_disabled": true                            // G-F (wire shape)
  }
}

201 Created
{
  "id": "...",
  "work_dir": "/tmp/tenant-project",
  "session_options": { ... echoed ... }
}
```

**Enforcement status:**

| Field | Status |
|---|---|
| `model` | Per-session enforcement live (G-G). Each turn for that session uses this model regardless of the App default. |
| `tools_allowlist` / `tools_blocklist` | Wire shape accepted and echoed; per-session enforcement is gated on **G-I** (decoupling memex namespaces from conversation namespaces) and is therefore documented but not enforced in v1. For real restriction today, use the operator-level flag described below. |
| `persona_disabled` | Same: wire shape live, per-session enforcement gated on G-I. Use `--no-persona` for operator-level disable. |

## Operator flags on `ycode serve`

Process-wide knobs the operator controls before any client connects:

```
ycode serve [--no-auth] [--no-persona]
            [--tools-allowlist=name1,name2]
            [--tools-blocklist=name1,name2]
```

| Flag | Effect |
|---|---|
| `--no-auth` | Permissive mode. The server token is not generated; `Authorization` headers are not validated; identity headers are still decoded (so single-tenant TUIs keep working). **Local dev only.** |
| `--no-persona` | Force-disable persona inference for every session, regardless of `cfg.PersonaEnabled` and `session_options.persona_disabled`. |
| `--tools-allowlist` | Register only these built-in tool names process-wide. Mutually exclusive with `--tools-blocklist`. |
| `--tools-blocklist` | Register every built-in tool except these. Ignored when `--tools-allowlist` is set. |

**Multi-tenant deployment pattern:** until G-I lands, run one
`ycode serve` per security profile when you need per-tenant tool
restrictions. For example:
- One serve for parents on port `:58080` started with
  `--tools-allowlist=read_file,grep_search,Agent` and
  `--no-persona`.
- One serve for admins on port `:58081` with default tool registry.

Both share the same model and embedding precedence, but each has its
own port + token.

## Caveats and follow-ups

- **Token rotation:** the server token is regenerated on every restart.
  Clients must re-read `~/.agents/ycode/server.token` after a reconnect.
  Stable tokens are a follow-up (see plan: "Permanent server tokens").
- **TLS:** the server binds 127.0.0.1 in plain HTTP. For cross-host
  deployment, terminate TLS at a reverse proxy (nginx, Caddy). Native
  TLS flags are tracked as G-K.
- **API versioning:** routes live at `/api/...` (no `/v1`). Until a
  versioning strategy lands (G-L), treat additive changes as the only
  guarantee — the server may add fields and routes but will not remove
  or rename them within a minor release.
- **Tenant-of-tenants (G-I):** decoupling per-tenant memex from the
  per-tenant conversation namespace is the load-bearing item that
  unlocks true per-session enforcement of tools_allowlist /
  persona_disabled. See the plan for scope.

## Quick verification

```bash
ycode serve &
TOKEN=$(cat ~/.agents/ycode/server.token)
PORT=$(cat ~/.agents/ycode/serve.port)

# Health check (no auth required).
curl -i http://127.0.0.1:$PORT/api/health

# Auth check: bare request rejected with 401.
curl -i http://127.0.0.1:$PORT/api/sessions -H 'X-Work-Dir: /tmp/x' -X POST

# Authenticated session create with options.
curl http://127.0.0.1:$PORT/api/sessions \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Work-Dir: /tmp/x" \
  -H "X-Actor-User: parent_47" -H "X-Actor-Roles: parent" \
  -X POST \
  -d '{"work_dir":"/tmp/x","session_options":{"model":"claude-haiku-4-5-20251001","persona_disabled":true}}'

# One-shot extract.
curl http://127.0.0.1:$PORT/api/extract \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Work-Dir: /tmp/x" \
  -d '{"prompt":"Return {\"ok\":true} as JSON","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}}'

# Single embedding.
curl http://127.0.0.1:$PORT/api/embed \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"text":"hello world"}'
```

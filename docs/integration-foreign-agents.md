# Integrating foreign agentic tools with ycode

This guide is for the operator of an external agentic coding tool — **claude
code, opencode, codex, gemini cli, ycode's own TUI**, or any other
MCP-speaking client — who wants that tool to reach ycode's capabilities
(Loom workspaces, embedded Gitea, Pulse observability, future memex / repo
map / inference) without modifying either project.

The contract on both sides is **public APIs only**:

- ycode exposes a single composite MCP endpoint at `${YCODE_URL}/mcp/`,
  HTTP discovery at `${YCODE_URL}/.well-known/ycode-manifest.json`, and
  OTLP ingest on standard ports. Nothing else is required of the client.
- The foreign tool stays unmodified — opencode is installed from its
  official channel, claude code from Anthropic's, etc. Configuration lives
  in the user's own config directory (`~/.opencode/`, `~/.mcp.json`,
  `~/.codex/config.toml`, `~/.gemini/settings.json`, ...), never inside
  the tool's source tree.

## Mental model: ycode is the "Agent OS"

ycode's server is the kernel/hub of an Agent OS. Every agentic tool that
connects to it — including ycode's own TUI — is a peer client. There is no
privileged path: ycode's TUI reaches Loom, Gitea, and Pulse through the
exact same `/mcp/` URL that opencode does. If you set `YCODE_URL` to a
remote host, the integration works identically.

This has one structural consequence worth stating up front: ycode does
not, and will not, ship tool-specific integration plumbing for the long
tail of clients. The only places per-client logic exists are (a) the
config-snippet emitter (`ycode pair --tool`) and (b) the matching
templates under `templates/<tool>/`. Adding a fifth recognized tool is a
deliberate ycode code change with a justification; the default answer for
any other client is **"point it at `/mcp/`."**

## Quick start (three commands)

```bash
# 1. Start ycode on the host that will provide the services.
ycode serve

# 2. On the client side, pair with the server:
ycode pair --tool opencode --url http://your-ycode-host:58080

# 3. Paste the printed snippet into the location the command shows
#    (e.g. ~/.opencode/opencode.jsonc), then launch your client.
```

After step 2 the foreign tool sees `ycode:loom_lease`, `ycode:list_repos`,
`ycode:query_traces`, etc. in its tool list. No further configuration.

## The public surface

| Endpoint | Auth | Purpose |
|---|---|---|
| `GET /.well-known/ycode-manifest.json` | none | Capability discovery. Returns URLs, MCP endpoint, version. No local paths or secrets. |
| `GET /manifest` | bearer | Full manifest including local-only fields (token paths, sandbox roots). |
| `POST /mcp/` | bearer | Composite MCP endpoint. JSON-RPC body. Fans out to every registered capability family. |
| `POST /loom-mcp/`, `/gitea-mcp/`, `/pulse/` | bearer | Individual capability families. Equivalent to `/mcp/` but pre-`<family>:` slicing. Kept for backward compat. |
| `:4317` / `:4318` | none | OTLP ingest (gRPC / HTTP). Standard OpenTelemetry endpoints. |

All non-OTLP endpoints listen on the proxy port (default `58080`). All
bearer-authenticated endpoints accept `Authorization: Bearer <token>`. The
token is whatever `ycode pair` printed; rotate by deleting
`~/.agents/ycode/server.token` and re-running `ycode pair`.

## Configuring specific clients

`ycode pair --tool <name>` prints the right snippet and tells you where it
goes. The recognized targets and their destinations:

| Tool | Destination | Format |
|---|---|---|
| `opencode` | `~/.opencode/opencode.jsonc` | JSONC `mcp` block |
| `claude-code` | `~/.mcp.json` or project `.mcp.json` | JSON `mcpServers` block |
| `codex` | `~/.codex/config.toml` | TOML `[mcp_servers.ycode]` block |
| `gemini-cli` | `~/.gemini/settings.json` | JSON `mcpServers` block |
| `ycode-tui` | shell init (`~/.zshrc` etc.) | `YCODE_URL` + `YCODE_TOKEN` env vars |

Pass `--tool <anything-else>` to get a generic snippet you can adapt.

### opencode

`ycode pair --tool opencode` produces:

```jsonc
{
  "mcp": {
    "ycode": {
      "type": "remote",
      "url": "${YCODE_URL}/mcp/",
      "headers": { "Authorization": "Bearer ${YCODE_TOKEN}" },
      "timeout": 30000
    }
  }
}
```

Drop it in `~/.opencode/opencode.jsonc` (or your project's
`.opencode/opencode.jsonc`). opencode auto-discovers MCP servers on
launch; restart opencode if it was already running.

### claude code

```json
{
  "mcpServers": {
    "ycode": {
      "type": "http",
      "url": "${YCODE_URL}/mcp/",
      "headers": { "Authorization": "Bearer ${YCODE_TOKEN}" }
    }
  }
}
```

Goes in `~/.mcp.json` (user-global) or `.mcp.json` at the repo root
(project-scoped).

### codex / gemini-cli / ycode-tui

See `ycode pair --tool <name>` output. Each is a one-paste configuration.

## Telemetry: opencode → Pulse

Any OTEL-emitting client can ship spans / metrics / logs to ycode's
collector:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://your-ycode-host:4318
export OTEL_SERVICE_NAME=opencode  # or claude-code, codex, ...
```

Pulse partitions by `service.name`, so opencode, claude code, codex, and
ycode itself appear as distinct services in the same dashboards. There is
no tool-specific code in Pulse — the OTEL resource attribute is the only
input.

## The Agent OS canary

ycode's own TUI is configured as a client using the exact same `ycode
pair --tool ycode-tui` flow. If you change your `YCODE_URL` to a remote
host, the TUI works against the remote ycode server identically to how it
works locally. This is by design: there is no in-process back-channel
that bypasses `/mcp/`. If you ever observe a behavior available to the
TUI but not to a foreign client through the public API, that is a bug —
please file it.

## Auth: today and tomorrow

**Today (v1)** — `ycode pair` reads or mints the single bearer token at
`~/.agents/ycode/server.token`. Operators distribute it to clients
out-of-band (paste it into the snippet, store as `YCODE_TOKEN` env var).
Rotating is `rm ~/.agents/ycode/server.token && ycode pair --tool <x>`.

**Later** — scoped per-tool tokens, device-code pairing for cross-host
flows, and MCP OAuth dynamic client registration (which opencode and
some other clients already support natively). The `ycode pair --tool`
CLI surface is the entry point for all of these; the snippet shape is
forward-compatible.

## Permission propagation (Agent OS capabilities)

Many clients have their own permission tiers — opencode's `plan` vs
`build` agents, claude code's `read-only` / `acceptEdits` /
`bypassPermissions` modes. ycode honors a client-provided per-request
permission hint via the MCP `_meta` field:

```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": { "name": "memex:store_memory", "arguments": { ... } },
  "_meta": { "permission": "ReadOnly" }
}
```

ycode's gate enforces `min(handler ceiling, client hint)`, so a client in
a restricted mode cannot accidentally invoke a write tool even if the
ceiling allows it. Use `_meta.permission` rather than custom HTTP headers
so the convention works across both HTTP and stdio MCP transports and any
spec-compliant client.

> This is the canonical "generic over tool-specific" change: zero
> opencode/claude code/codex references in the implementation; benefits
> every MCP client that has its own permission concept.

## Troubleshooting

- **"Connection refused" from the client.** ycode server isn't running, or
  `YCODE_URL` points at the wrong host. Check
  `curl ${YCODE_URL}/.well-known/ycode-manifest.json`.
- **"Unauthorized" from `/mcp/` or `/manifest`.** Bearer token mismatch.
  Re-run `ycode pair --tool <name>` and update the client's config.
- **Tool list empty.** ycode serve hasn't finished bringing up the
  capability families. Check `ycode serve status`; the composite endpoint
  is mounted last.
- **Client doesn't auto-refresh tool list when ycode adds a family.**
  opencode honors MCP `tools/list_changed`; claude code and codex do not.
  Restart the client.

## What's intentionally out of scope

- **Plugins, SDKs, or npm packages.** None of the supported integrations
  require installing additional code on the client side. If a specific
  perf bottleneck ever justifies a plugin (e.g., very high tool-call
  rates), it will be a separate, optional artifact — not a v1 path.
- **Modifying the foreign tool.** opencode, claude code, codex, and
  gemini cli are installed from their official channels and never
  touched. ycode does not write into a foreign tool's source tree.
- **Filesystem co-location.** Nothing in this guide assumes ycode and the
  client are on the same host. `YCODE_URL` is the only address; replace
  `127.0.0.1` with any reachable host and the integration is unchanged.

## Related docs

- [lighthouse.md](./lighthouse.md) — the "ycode as MCP federation hub"
  design rationale, of which this integration guide is the operational
  manifestation.
- [lighthouse-roadmap.md](./lighthouse-roadmap.md) — the queue of future
  capability families (memex MCP, repomap MCP, inference MCP, sandbox
  MCP, browser MCP) that will appear under `/mcp/` as ycode ships them.
  No client-side config change is required as families come online —
  opencode et al. just see new tools appear in `tools/list`.
- [loom.md](./loom.md) — workspace substrate, the five `loom_*` MCP
  tools exposed today.

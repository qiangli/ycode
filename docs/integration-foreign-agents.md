# Integrating foreign agentic tools with ycode

This guide is for the operator of an external agentic coding tool — **claude
code, opencode, codex, gemini cli, ycode's own TUI**, or anything else that
shells out — who wants that tool to reach ycode's capabilities without
modifying either project.

> **ycode does not speak MCP, in either direction.** It exposes no MCP
> server, mounts no `/mcp/` route, and connects to none. If you are
> holding an older snippet that adds a `ycode` entry to a tool's
> `mcpServers` map, delete it — nothing is listening, and every foreign
> CLI configured that way reports a failed server at startup. See
> [plan-remove-mcp.md](./plan-remove-mcp.md).

The integration surface is the **shell**. Point a foreign tool's bash
backend at `ycode shell -c` and ycode's capabilities resolve in-process
as `yc <verb>` built-ins — treesitter AST search, repo map, code graph,
semantic memory, native git — with no server, no daemon, and no auth.

The contract on both sides is **public APIs only**:

- ycode exposes the `yc <verb>` built-ins through `ycode shell`, an HTTP
  API at `${YCODE_URL}/ycode/`, HTTP discovery at
  `${YCODE_URL}/.well-known/ycode-manifest.json`, and OTLP ingest on
  standard ports.
- The foreign tool stays unmodified — opencode is installed from its
  official channel, claude code from Anthropic's, etc. Configuration
  lives in the user's own config directory or in `$PATH`, never inside
  the tool's source tree.

## Mental model: ycode is the "Agent OS"

ycode is the local substrate every agentic tool runs *on top of*. The
reach is lexical, not networked: a tool whose `bash` resolves to
`ycode shell` gets the whole `yc` verb set for free, in the same process,
at bash latency. There is no privileged path — ycode's own TUI uses the
same built-ins any foreign tool does.

One structural consequence: ycode does not, and will not, ship
tool-specific integration plumbing for the long tail of clients. The only
per-client logic is (a) the config-snippet emitter (`ycode pair --tool`)
and (b) the L2 memory-block writers in `internal/selfinit/`. The default
answer for any other client is **"point its bash at `ycode shell -c`."**

## Quick start

```bash
# 1. In the repo you're working in, make ycode a first-class citizen.
#    Writes the capability block into AGENTS.md / CLAUDE.md so the
#    foreign tool's LLM knows the `yc` verbs exist.
ycode init

# 2. Install a session-scoped PATH wrapper so the tool's `bash` is ycode.
mkdir -p ~/bin/ycode-wrappers
printf '#!/usr/bin/env -S ycode shell --agent\n' > ~/bin/ycode-wrappers/bash
chmod +x ~/bin/ycode-wrappers/bash
ln -sf bash ~/bin/ycode-wrappers/zsh

# 3. Launch the foreign tool with the wrapper in front.
PATH="$HOME/bin/ycode-wrappers:$PATH" claude     # or opencode, codex, ...
```

Verify from inside the tool: ask it to run `yc symbols ./internal`. If it
returns declarations rather than `command not found`, the wrapper is live.

The full recipe, including the trace-log check for tools that don't
`spawn('bash', …)`, is in [shell-agent.md](./shell-agent.md).

## The public surface

| Endpoint | Auth | Purpose |
|---|---|---|
| `ycode shell -c "<cmd>"` | none (local process) | The integration surface. `yc <verb>` built-ins resolve in-process. |
| `GET /.well-known/ycode-manifest.json` | none | Capability discovery. Returns URLs and version. No local paths or secrets. |
| `GET /manifest` | bearer | Full manifest including local-only fields (token paths). |
| `/ycode/` | bearer | ycode's HTTP API, served by `ycode serve`. |
| `:4317` / `:4318` | none | OTLP ingest (gRPC / HTTP). Standard OpenTelemetry endpoints. |

All non-OTLP endpoints listen on the proxy port (default `31415`). All
bearer-authenticated endpoints accept `Authorization: Bearer <token>`. The
token is whatever `ycode pair` printed; rotate by deleting
`~/.agents/ycode/server.token` and re-running `ycode pair`.

`ycode serve` is only needed for the HTTP API and telemetry. The shell
integration works with no server running at all.

## Configuring specific clients

`ycode pair --tool <name>` prints the right snippet and tells you where it
goes. Recognized targets: `opencode`, `claude-code`, `codex`,
`gemini-cli`, `ycode-tui`. Pass `--tool <anything-else>` for a generic
snippet you can adapt.

For all four foreign CLIs the snippet is the same shape — env vars for
the HTTP API plus a pointer at the shell path. There is no config-file
block to paste into the tool's own settings, because ycode has nothing
to register there.

- **claude code** — full recipe in
  [integration-claude-code.md](./integration-claude-code.md).
- **opencode** — full recipe in
  [integration-opencode.md](./integration-opencode.md).
- **codex / gemini-cli** — same PATH-wrapper recipe; both resolve `bash`
  through `$PATH` when executing tool calls.
- **ycode-tui** — `ycode pair --tool ycode-tui` prints the `YCODE_URL` /
  `YCODE_TOKEN` env pair; launch with `ycode --connect $YCODE_URL/ycode/`.

`ycode init` additionally splices a delimited capability block into each
detected tool's user-scope memory file (`~/.claude/CLAUDE.md`,
`~/.config/opencode/AGENTS.md`) so the LLM knows the verbs exist without
being told per session. It writes no tool-config file. See
[selfinit.md](./selfinit.md).

## Telemetry → Pulse

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

## Auth: today and tomorrow

**Today (v1)** — `ycode pair` reads or mints the single bearer token at
`~/.agents/ycode/server.token`. Operators distribute it to clients
out-of-band (paste it into the snippet, store as `YCODE_TOKEN` env var).
Rotating is `rm ~/.agents/ycode/server.token && ycode pair --tool <x>`.
The token gates the HTTP API only; the shell path needs no token because
it is a local process, governed by ycode's own permission modes.

**Later** — scoped per-tool tokens and device-code pairing for cross-host
flows. The `ycode pair --tool` CLI surface is the entry point for both.

## Permission posture

The foreign tool's own permission tiers apply first — opencode's `plan`
vs `build` agents, claude code's `default` / `acceptEdits` /
`bypassPermissions` modes. A denied tool call never reaches ycode.

Underneath, `ycode shell -c` defaults to `DangerFullAccess` — the same
posture as `/bin/bash`, because surprising an agent with a restricted
shell breaks its existing scripts. Tighten per invocation:

```bash
ycode shell -c --permission read-only "ls /etc"
ycode shell -c --permission workspace-write "./build.sh"
```

## Troubleshooting

- **Foreign CLI reports a failed `ycode` MCP server at startup.** You
  have a stale `mcpServers` entry. Remove it; ycode runs no MCP server.
- **`yc: command not found` inside the tool.** The PATH wrapper isn't
  being used. Trace it with a logging wrapper (see
  [shell-agent.md](./shell-agent.md)) — some tools exec `/bin/sh` or a
  hardcoded shell path and can't be intercepted this way.
- **"Connection refused" hitting the HTTP API.** `ycode serve` isn't
  running, or `YCODE_URL` points at the wrong host. Check
  `curl ${YCODE_URL}/.well-known/ycode-manifest.json`.
- **"Unauthorized" from `/ycode/` or `/manifest`.** Bearer token
  mismatch. Re-run `ycode pair --tool <name>`.

## What's intentionally out of scope

- **MCP.** Removed entirely, both as a server and as a client. Not a
  gap to be filled; a decision. See [plan-remove-mcp.md](./plan-remove-mcp.md).
- **Plugins, SDKs, or npm packages.** No supported integration requires
  installing additional code on the client side.
- **Modifying the foreign tool.** opencode, claude code, codex, and
  gemini cli are installed from their official channels and never
  touched. ycode does not write into a foreign tool's source tree.

## Related docs

- [shell-agent.md](./shell-agent.md) — the canonical integration recipes
  and the agent-mode hint engine.
- [selfinit.md](./selfinit.md) — how `ycode init` makes a repo and the
  installed foreign tools ycode-aware.
- [lighthouse.md](./lighthouse.md) — the design rationale for ycode as
  the local capability hub.

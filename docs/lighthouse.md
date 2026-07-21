# The Lighthouse Pattern

ycode bundles capabilities most coding agents do not have: polyglot AST
search, a code-knowledge graph, semantic memory, an embedded Ollama for
cheap subtasks, an OTEL collector, the skills registry. The lighthouse
pattern makes those capabilities reachable from any third-party coding
agent (Claude Code, opencode, Codex, Gemini CLI, an older ycode build)
without plugins and without a server.

> **The MCP implementation of this pattern has been removed.** ycode
> once shipped a stdio MCP server (`ycode mcp serve`) and an HTTP
> composite endpoint at `/mcp/`. Neither exists: there is no `mcp`
> subcommand, no `/mcp/` route, no `mcp.ServerHandler` interface, and no
> `.mcp.json` at the repo root. Any config that names a `ycode` MCP
> server is stale and will fail at the client's startup. See
> [plan-remove-mcp.md](./plan-remove-mcp.md).

## The surviving beam: the shell

The pattern still holds; the transport changed. ycode's capabilities are
reachable as **`yc <verb>` shell built-ins**, dispatched in-process by
`ycode shell` before `$PATH` lookup. Any agent whose bash backend
resolves to `ycode shell` gets them with zero configuration, zero
handshake, and zero daemon:

```bash
mkdir -p ~/bin/ycode-wrappers
printf '#!/usr/bin/env -S ycode shell --agent\n' > ~/bin/ycode-wrappers/bash
chmod +x ~/bin/ycode-wrappers/bash
PATH="$HOME/bin/ycode-wrappers:$PATH" claude   # or opencode, codex, ...
```

Why this is a better beam than an MCP server was:

- **No protocol negotiation.** A tool call is a shell command. Every
  agent already has one.
- **No failure surface.** Nothing to start, nothing to authenticate,
  nothing to report as "disconnected" at launch.
- **Unshadowable.** Built-ins dispatch before `$PATH`, so no host binary
  can intercept `yc symbols`.
- **Discoverable by the model.** `yc help` / `yc manifest` and
  `ycode docs <topic>` are self-describing, and `ycode init` splices the
  capability list into the agent's own memory file.

## Adding a capability

Adding a new ycode capability to the lighthouse surface is one file:

1. **Pick the underlying capability.** It already exists in `internal/…`
   or `pkg/…` — you are wrapping, not reimplementing.
2. **Implement the built-in interface in `internal/shell/builtins/`**
   (`Name`, `Description`, `Usage`, `Run`), and `Register` it from an
   `init()`. `treesitter.go` and `repomap.go` are the canonical
   templates.
3. **Emit structured output** behind `--json` using the envelope in
   `internal/shell/envelope.go` when exit code and duration are data.
4. **Add a hint** in `internal/shell/agentmode/` if a common non-ycode
   command (`grep -rn`, `ctags -R`) should route to the new verb. Every
   hint carries a `Why:` line.
5. **Add an agent doc** under `internal/docs/agent/` and a catalog row in
   `internal/docs/catalog.yaml` so `ycode docs` and `yc manifest` both
   surface it. `go test ./internal/docs/...` is the curation gate.

Stable interface, copy-pasteable, no orchestration to learn.

## Permission posture

`ycode shell -c` defaults to `DangerFullAccess` — the same posture as
`/bin/bash`, because surprising an agent with a restricted shell breaks
its existing scripts. The foreign tool's own permission tier gates the
call before it ever reaches ycode. Tighten per invocation:

```bash
ycode shell -c --permission read-only "ls /etc"
ycode shell -c --permission workspace-write "./build.sh"
```

## Federation discipline

Architectural rule, non-negotiable: **ycode is the hub of *your* matrix,
not the matrix's hub.** Each user runs ycode locally. ycode never phones
home. Two ycode instances can optionally federate (peer-to-peer) but
federation is opt-in. The mental model is email/git/ActivityPub —
federated, never centralized. Centralized routing would contradict the
wedge and make ycode a chokepoint competitors route around.

## What's exposed today

Shell built-ins (`yc help` for the live list): `symbols`,
`search-symbols`, `refs`, `repomap`, `graph`, `git`, `remember`,
`recall`, `test`, `lsp`, `run`, `qacache`, `sandbox` (delegating stub),
`help`, `manifest`.

HTTP, via `ycode serve`: the `/ycode/` API, `/manifest`,
`/.well-known/ycode-manifest.json`, and OTLP ingest at
`127.0.0.1:4317` (gRPC) / `:4318` (HTTP) — any process can ship spans
into ycode's collector.

The manifest at `~/.agents/ycode/manifest.json` advertises exactly what
the binary serves. It lists no MCP servers.

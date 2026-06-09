---
topic: sandbox
summary: run a command in a podman-isolated alpine container
when: a tool wants to run untrusted code, or you need network=none isolation
audience: agent
max_lines: 80
---

`yc sandbox` runs an arbitrary command inside a fresh podman container
(image: alpine, network=none, cwd mounted at `/workspace`). The
container is destroyed after the command exits. Use it whenever you
want a command's filesystem and network blast radius constrained to a
disposable surface.

## When to use this

- A user-supplied script needs to execute and you don't fully trust it.
- You want to verify a build / test step in clean isolation, no DNS,
  no stray env vars from the parent shell.
- You're an automated agent loop and want a defence-in-depth boundary
  between the model's suggestions and the user's filesystem.

## What it does NOT do

- It is NOT a long-running container — one command per call.
- It has NO network access by default. Tools that need to fetch
  packages will fail; that's the point.
- It is NOT available where `podman` is missing. Falls back to a
  clear error; nothing silently runs unsandboxed.

## Failure modes

| Symptom | Fix |
|---|---|
| `podman: command not found` | Install podman; sandbox can't start. |
| Command fails with DNS errors | Expected — `--network=none` blocks resolution. |
| File written inside sandbox vanished | Sandbox cwd is `/workspace`, mounted from your cwd; writes there persist, writes elsewhere don't. |
| `permission denied` accessing /Users/... | Only the cwd is mounted; sibling directories aren't visible. |

## Exact calls

- One-shot exec: `yc sandbox -- python3 unsafe.py`
- With args + redirect (let the host shell handle the redirect):
  `yc sandbox -- sh -c 'tar tzvf incoming.tgz | head'`
- Test isolation: `yc sandbox -- go build ./...` (will fail offline if
  module cache misses — confirm modules are pre-fetched first).
- MCP equivalent (works on both stdio and HTTP transports):
  `mcp__ycode__sandbox_exec` with `{command: "python3 unsafe.py"}`.

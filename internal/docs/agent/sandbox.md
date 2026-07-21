---
topic: sandbox
summary: isolated command execution — delegated outside ycode
when: a tool wants to run untrusted code, or you need network=none isolation
audience: agent
max_lines: 80
---

Lean ycode does not execute sandboxed commands itself. The `yc sandbox`
built-in is a compatibility stub: it validates its arguments and then
exits non-zero with

```
yc sandbox: not available in lean ycode; run ycode under bashy or
another external sandbox wrapper
```

The former MCP `sandbox_exec` tool no longer exists — ycode neither
exposes nor consumes MCP, so there is nothing to fall back to on that
path either.

## When to use this

Read this topic to decide *where* to run an isolated command, then run
it there:

- **Under the AgentOS shell** — `bashy` embeds the podman engine
  (`bashy podman …`), so an isolated container (alpine,
  `--network=none`, cwd mounted at `/workspace`) is one command away.
  This is the supported path.
- **Under any external wrapper you already trust** — a `podman run`
  / `docker run` invocation issued through `bash`, a devcontainer, a VM.
  ycode has no opinion; it just won't do it for you.

If neither is available, say so and do not silently run the command
unsandboxed. That is the whole point of the boundary.

## What ycode still guarantees

- `yc sandbox` never runs the command on the host. Failure is loud and
  the exit code is non-zero; nothing degrades to an unisolated run.
- ycode's own permission modes (ReadOnly / WorkspaceWrite /
  DangerFullAccess) still gate `bash` and the file tools. Sandboxing is
  defence in depth on top of that, not a replacement for it.

## Failure modes

| Symptom | Fix |
|---|---|
| `yc sandbox: not available in lean ycode` | Expected. Run the command through `bashy` or an external container wrapper. |
| Looking for `sandbox_exec` in your tool list | Removed with MCP. There is no replacement tool inside ycode. |
| Command fails with DNS errors under an external sandbox | Expected when the wrapper sets `--network=none`. |
| File written inside the container vanished | Only the mounted cwd persists; writes elsewhere die with the container. |

## Exact calls

- Check the stub's contract: `yc sandbox -- true` (exits 1 with the
  delegation message; useful to confirm you are on lean ycode).
- Supported isolated run, via the sibling shell's embedded engine:
  `bashy podman run --rm --network=none -v "$PWD:/workspace" -w /workspace alpine python3 unsafe.py`
- Raw external wrapper, no ycode involvement:
  `podman run --rm --network=none -v "$PWD:/workspace" -w /workspace alpine sh -c 'tar tzvf incoming.tgz | head'`

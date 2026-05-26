# ycode capabilities — agent index

You are reading this because something invoked `ycode docs` (or
`mcp__ycode__docs` with no arg). This file is the curated entry point
to every ycode capability that exposes a verb you can call.

**Drill into a topic:** `ycode docs <topic>` (or `mcp__ycode__docs({topic: "<topic>"})`).
**Dump everything for a system prompt:** `ycode docs --all` (one-time, not per turn).
**Machine-readable index:** `ycode docs --list` (JSON).

## Topics

- **mcp** — connect to ycode's MCP composite endpoint (HTTP) or stdio
  server. Start here if you've never used ycode before; most other
  capabilities are reachable as MCP tools.

> Phase 0 of the docs registry ships one topic. New entries land here as
> each capability earns a verb worth surfacing to agents. The rule is
> deliberate: a topic earns a doc only when an agent has a verb to
> perform — not because a feature is interesting to humans.

## Sibling surfaces (read the right one)

- For the **list of every cobra subcommand and its flags** (human
  audience), run `ycode help` or `ycode <cmd> --help`. The docs you are
  reading are agent-facing prose; `help` is structural CLI metadata.
- For the **`yc <verb>` shell built-ins** (active inside `ycode shell`
  or any bash that routes through `ycode shell -c`), run `yc help` or
  `yc manifest`.
- For **live endpoint discovery** (URLs, ports, tokens for a running
  `ycode serve`), read `~/.agents/ycode/manifest.json`.

The docs registry and the cobra help tree cross-reference each other
but never share content. If a fact about a verb appears in both places
and they disagree, the cobra command's `--help` output is authoritative
(it's generated from the live binary); file a fix against the agent doc.

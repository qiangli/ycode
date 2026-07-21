# ycode capabilities — agent index

You are reading this because something invoked `ycode docs`. This file
is the curated entry point to every ycode capability that exposes a
verb you can call.

**Drill into a topic:** `ycode docs <topic>`.
**Dump everything for a system prompt:** `ycode docs --all` (one-time, not per turn).
**Machine-readable index:** `ycode docs --list` (JSON).

ycode has two callable surfaces: the `yc <verb>` shell built-ins
(in-process, active in any bash that routes through `ycode shell -c`)
and ycode's own in-session tools. There is no ycode MCP server — ycode
neither exposes nor consumes MCP; do not try to configure one.

## Topics

- **code-exploration** — AST-aware code search and orientation:
  `yc symbols`, `yc search-symbols`, `yc refs`, `yc repomap`. Use
  before reaching for `grep -rn` / `find -name '*.go'` / `ctags -R`
  on Go / Python / JS-TS / Rust / Java / C / Ruby sources.
- **repomap** — token-budgeted repo orientation (`yc repomap`).
- **memex** — semantic memory: save / recall / list / forget, via
  `yc remember` / `yc recall` and the in-session `memory_*` tools.
- **sandbox** — isolated command execution. Delegated outside lean
  ycode; read the topic before assuming it runs.
- **loom** — where the parallel-workspace fan-out went. ycode no
  longer ships it; the surface is the sibling `bashy weave` command.
- **structured-output** — typed JSON envelopes for tests, LSP queries,
  and arbitrary commands: `yc test --json`, `yc lsp <action> --json`,
  `yc run --json`. Use when exit-code + duration are data, not text.

> New entries land here as each capability earns a verb worth surfacing
> to agents. The rule is deliberate: a topic earns a doc only when an
> agent has a verb to perform — not because a feature is interesting
> to humans.

## Sibling surfaces (read the right one)

- For the **list of every cobra subcommand and its flags** (human
  audience), run `ycode help` or `ycode <cmd> --help`. The docs you are
  reading are agent-facing prose; `help` is structural CLI metadata.
- For the **`yc <verb>` shell built-ins** (active inside `ycode shell`
  or any bash that routes through `ycode shell -c`), run `yc help` or
  `yc manifest`.
- For **live endpoint discovery** (URLs, ports, tokens for a running
  `ycode serve`), read `~/.agents/ycode/manifest.json`. It advertises
  HTTP endpoints only — no MCP servers.
- For **parallel subagent orchestration**, use the sibling AgentOS shell
  surface: `bashy weave guide`.

The docs registry and the cobra help tree cross-reference each other
but never share content. If a fact about a verb appears in both places
and they disagree, the cobra command's `--help` output is authoritative
(it's generated from the live binary); file a fix against the agent doc.

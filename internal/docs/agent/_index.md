# ycode capabilities — agent index

You are reading this because something invoked `ycode docs` (or
`mcp__ycode__get_doc` with no `topic` arg). This file is the curated
entry point to every ycode capability that exposes a verb you can call.

**Drill into a topic:** `ycode docs <topic>` (or `mcp__ycode__get_doc({topic: "<topic>"})`).
**Dump everything for a system prompt:** `ycode docs --all` (one-time, not per turn).
**Machine-readable index:** `ycode docs --list` (JSON).

## Topics

- **mcp** — connect to ycode's MCP composite endpoint (HTTP) or stdio
  server. Start here if you've never used ycode before; most other
  capabilities are reachable as MCP tools.
- **browser** — drive a real or headless browser (live / probe / solo)
  via the `browser_*` MCP tools; recovery patterns for AUTH_REDIRECT
  and BLOCKED.
- **tab** — drive the user's currently-open Chrome tab in live mode
  (extract / screenshot / navigate / click / type / scroll). The
  bridge to surfaces a headless browser can't reach.
- **weave** — orchestrate parallel agentic subagents (codex,
  claude-code, opencode, …) via a local queue + git worktrees.
  Add issues → background `start`s → `wait --all` → `pull`. Two
  exact-call patterns: parallel-impl-only and parallel-impl +
  judge-validation. Default backend; no `ycode serve` needed.
- **loom** — v1 substrate underneath weave: per-call MCP verbs
  for lease / push / merge / release. Most orchestrators want
  the weave CLI above instead.
- **outcomes** — taxonomy reference for the `outcome_class` field on
  every browser_* result.
- **sandbox** — `yc sandbox` podman-isolated command execution.
- **memex** — semantic memory: save / recall / list / forget.
- **repomap** — token-budgeted repo orientation (`yc repomap`).
- **code-exploration** — AST-aware code search and orientation:
  `yc symbols`, `yc search-symbols`, `yc refs`, `yc repomap`. Use
  before reaching for `grep -rn` / `find -name '*.go'` / `ctags -R`
  on Go / Python / JS-TS / Rust / Java / C / Ruby sources.
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
  `ycode serve`), read `~/.agents/ycode/manifest.json`.

The docs registry and the cobra help tree cross-reference each other
but never share content. If a fact about a verb appears in both places
and they disagree, the cobra command's `--help` output is authoritative
(it's generated from the live binary); file a fix against the agent doc.

---
title: Add memory + graph capability families to ycode mcp serve (lighthouse Phase 1)
priority: p1
state: open
created: 2026-05-10T00:00:00Z
acceptance:
  - bin/ycode mcp serve exposes a memory family (recall/remember) routed through pkg/memex/memory/
  - bin/ycode mcp serve exposes a graph family (query_graph_dql) routed through pkg/memex/graph/
  - Both families are registered via mcp.NewCompositeHandler per the recipe in docs/lighthouse.md
  - A foreign agent (Claude Code) opening this tree can list both families via tools/list and invoke at least one tool from each end-to-end
gitea_issue: 2
---

## Context

`bin/ycode mcp serve` currently ships infrastructure only — empty
`tools/list`. The next-after-Phase-0 step is wiring the queued
capability families per `docs/lighthouse.md` and tracked in
`docs/lighthouse-roadmap.md`. The two highest-leverage families are
**memory** (so foreign agents share recall context with ycode's
five-layer memex) and **graph** (so they can DQL-query the bonsai
code-knowledge graph).

## Approach

1. Add `internal/runtime/mcp/memoryserver.go` exposing `recall` and
   `remember` thin wrappers over `pkg/memex/memory/`.
2. Add `internal/runtime/mcp/graphserver.go` exposing `query_graph_dql`
   over `pkg/memex/graph/`.
3. Plug both into the composite handler registered by
   `bin/ycode mcp serve`.

## Verification

End-to-end: open Claude Code in the ycode tree, the `.mcp.json`
lighthouse beam auto-registers ycode's MCP server, `tools/list` shows
the new tools, and a chat invocation of `recall <query>` returns
results from this user's memex.

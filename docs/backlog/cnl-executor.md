---
title: Implement external_cnl skill executor
priority: p1
state: open
created: 2026-05-10T00:00:00Z
acceptance:
  - "Catalog skills declaring `executor: cnl` resolve via the dispatcher (no longer return source=external_cnl with err_kind=not_supported)"
  - "Invocation translates the CNL body through internal/runtime/skillcnl/ to the dhnt IR before execution"
  - "At least one catalog skill ships with `executor: cnl` and is exercised in a smoke test"
  - "docs/skill-cnl.md and docs/skill-cnl-rationale.md are updated with the dispatch path"
gitea_issue: 1
---

## Context

The skill-CNL Phase 0 alpha lives at `internal/runtime/skillcnl/` and
the multilingual encoder + glossary + AST roundtrip ship under that
prefix (commit `37b664e`). The dhnt catalog has slots for skills that
declare `executor: cnl`, but the dispatcher currently surfaces them as
`source=external_cnl` with `err_kind=not_supported` — the integration
between the dispatcher and the CNL transpiler is missing.

## Approach

1. Add a `cnl` executor branch in the skill dispatch path. When a
   catalog entry has `executor: cnl`, route through
   `internal/runtime/skillcnl/` to translate the CNL body into the
   dhnt IR and execute that.
2. Pick a small daily-tier catalog skill (or author one) with
   `executor: cnl` so we have a real exercise.
3. Wire a smoke test in `internal/runtime/skillcnl/`.

## Why this is p1

Without it, every CNL skill in the catalog is dead weight during the
dogfood week. Anything authored for the multilingual story shows up
as a failure in the telemetry instead of a signal.

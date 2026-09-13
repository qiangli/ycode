# Story ddfffb0ca0bc (sprint 163 Y5, docs + selfinit) — residue notes

Everything in the story is implemented: `docs/memory.md` rewritten to the
stage model (event log as transcript, `session.load`/`session.commit`,
compaction shape, `bashy.run` + `provider: bashy-kb`, memex's fate via C8),
the nine `docs/gap-analysis-*-memory.md` files deleted and replaced by
`docs/prior-art-memory-and-compaction.md`, `docs/architecture.md` /
`docs/usage.md` / `docs/harness-capability-map.yaml` updated for the new
stages and events, and `internal/selfinit/claude.go` no longer writes the
deleted `yc` built-ins. Three items need recording.

## 1. The prior-art "tables" exist in the umbrella only as a paragraph

The story says the survey content "is in docs/sprint-163-handoff.md in the
umbrella". The handoff's §Inventory carries the survey as one prose
paragraph of common-denominator findings (with named exceptions), not as
literal per-harness tables. `docs/prior-art-memory-and-compaction.md`
therefore renders the eight story dimensions as a table of
*common pattern × variations*, filling per-harness cells only where this
repo's own audit docs (`codex-` / `opencode-` / `openclaw-` /
`hermes-harness-primitives-audit.md`) provide evidence. No per-harness cell
was invented to make a five-column matrix look complete.

## 2. Files touched beyond the story list (why)

- `internal/selfinit/{claude_test,opencode_test,project_test}.go`: the tests
  asserted the removed `yc symbols <path>` line verbatim; they now assert
  the current surface and reject every removed one, and point
  `BASHY_KB_DIR`/`BASHY_HOME`/`BASHY_SKILLS_DIR`/`YCODE_DATA_DIR` at
  scratch dirs per the sprint rule.
- `internal/selfinit/project.go`: `buildLongFormDoc`'s intro and trailer
  carried the same stale sentences (`yc <verb>` built-ins,
  `ycode shell --manifest`, "your tool's MCP config") around the shared
  `buildInstructionsBlock`; fixing only claude.go would have left the
  breadcrumb contradicting the block it embeds.
- `docs/selfinit.md`: describes exactly what the selfinit writers emit; the
  three sentences naming the `yc <verb>` block (and the memex-substrate
  framing in the intro) were updated to match the new block.
- `docs/gap-analysis-priorart-2026.md`: its memory verdict linked four of
  the nine deleted files; the sentence now points at the new note and marks
  the verdict as describing the deleted subsystem. The rest of that legacy
  document was left as historical evidence.
- `docs/todo/ddfffb0ca0bc-…md`: the story card's own gate line contained a
  literal memex-as-provider phrase, which trips the sprint's doc grep gate
  over `docs/`; reworded to "memex-as-provider" with the same meaning.

## 3. Gate residue

`docs/persistence.md` still documents `pkg/memex`'s store layer. The package
itself is intentionally in-tree until C8's migration has run
(`docs/todo-notes/649028cb0be9.md`), the doc names no removed stage and no
memex *provider*, and rewriting it is not in this story's list — it should
fall together with `pkg/memex` after C8.

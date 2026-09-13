---
id: ddfffb0ca0bc
kind: task
title: 'Y5 docs: memory.md rewrite to the stage model; nine gap-analysis files -> one prior-art note; architecture/usage/capability map; remove stale yc lines from selfinit'
seq: 41
status: done
priority: p2
created: 2026-09-13T01:31:42.277282Z
weave: 5
assignee: qiangli
sprint: 163
closed: 2026-09-13T04:18:00.921056Z
---

Goal: ycode docs match the stage model.

- docs/memory.md (662 lines; describes the deleted runtime: Memos, 5-layer model, persona, dreaming) rewritten to: event log as transcript, session.load/commit, compaction shape, bashy.run + provider bashy-kb, what memex was and where it went (C8).
- The nine docs/gap-analysis-*-memory.md files collapse into one docs/prior-art-memory-and-compaction.md carrying the survey tables (Codex/OpenCode/OpenClaw/Hermes/open-claude-code: transcript store, load-at-turn, compaction shape, resume, fork, memory write, token measurement, summary prompts) — the content is in docs/sprint-163-handoff.md in the umbrella.
- docs/architecture.md, docs/usage.md ("continues from the boundary" now means conversational continuity), docs/harness-capability-map.yaml (new stages/events).
- internal/selfinit/claude.go still writes the deleted yc symbols/refs/repomap/graph lines into CLAUDE.md — remove them.

Gate: go test ./internal/selfinit/...; no doc references a removed stage or memex-as-provider; umbrella story U2 updates the index side.
Depends on: Y1-Y4.

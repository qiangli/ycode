---
id: 180afb01a609
kind: task
title: Cut consumer package references to separated source repositories
seq: 62
status: wontfix
created: 2026-11-18T04:14:29.78694Z
assignee: s575-consumer-refs
sprint: 575
sprint_id: 65ce85f5-87c0-5863-bf55-62de86f5df36
sprint_title: 'Layer separation: Classic <- Bash# <- Yoke import-graph delineation'
closed: 2026-11-22T13:41:43.60887Z
---

WONTFIX (operator rescope 2026-11-22; Sprint 575 ended): superseded. The shared-engine split is future refactoring the operator has not authorized; Sprint 677 delivered the certified-core boundary as bsh (bashsharp/cmd/bsh, zero yoke). Reopen only under a new, operator-authorized separation sprint. The standalone receipt gap stays recorded in umbrella docs/sprint-656-release-handoff.md.

Sprint 575 reference-only repository cutover. Preserve implementation and tests; update package/module references and fixed published lower dependency pins. Official-image go build is performed by the manager; no local Go execution.

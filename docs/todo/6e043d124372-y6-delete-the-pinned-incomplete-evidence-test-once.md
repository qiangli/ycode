---
id: 6e043d124372
kind: task
title: Y6 delete the pinned incomplete-evidence test once harnessrunner proves pure-builtin env consumption; assert the bashy.run env node succeeds under the real boundary
seq: 42
status: done
priority: p2
created: 2026-09-13T05:23:18.863656Z
sprint: 164
closed: 2026-09-13T05:35:20.466254Z
---

Sprint 163 Y1 recorded (docs/todo-notes/fe824e92dc23.md §1) that the sibling bashy/pkg/harnessrunner marks any variable expansion in a command argument unprovable, and pinned that with TestBashyRunEnvExpansionIsIncompleteEvidenceUnderRealBoundary ('expected to flip when a bashy-side refiner lands; delete that test then'). Sprint 164 B4 lands that refiner in bashy.

DO: after bashy B4 is on main and ycode's .sibling-pins / go.mod sibling points at it: delete TestBashyRunEnvExpansionIsIncompleteEvidenceUnderRealBoundary; add TestBashyRunEnvConsumptionSucceedsUnderRealBoundary — a bashy.run node whose script is printf '%s' "$YCODE_IN_TASK" under a read ceiling: preflight completes, the stage runs, stdout lands in the typed out port. Update docs/todo-notes/fe824e92dc23.md §1 to say the refiner landed (bashy commit). The canonical examples/agent.yaml fallback forms for knowledge/remember STAY (they are still the honest shape when bashy is older than the refiner).
Gate: go test -short -race ./internal/harness/... ./pkg/ycode; ./scripts/fmtcheck.sh. Depends on: bashy B4 merged + pinned.

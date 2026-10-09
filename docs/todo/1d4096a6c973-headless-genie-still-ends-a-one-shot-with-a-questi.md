---
id: 1d4096a6c973
kind: bug
title: headless genie still ends a one-shot with a question (GLM ignores the act-not-ask instruction); add a bounded auto-continue when no human is available
seq: 54
status: todo
priority: p1
labels:
    - genie
created: 2026-10-09T16:43:28.994215Z
sprint: 379
sprint_id: 908c2ac2-e7bc-55fe-86bd-d25046ac4684
sprint_title: 'bashy 1.0.0 feature list: bash + Bash# + Yoke'
---

S379 2026-10-09 live, weave yoke run 23, genie glm-5.3, after ycode 9f6435a/4bdafeb/7e918ef/91b15fd (context loads, no compaction loop, tool results kept): the brief named the fix (Windows: stop the door with Process.Kill), the model read the right code in 146 s, wrote a correct analysis, then ended the turn: 'Where do you want to take this - tighten down, fix the Windows story, or add the missing live lifecycle test first?' and exited 0 with no edit. The act-not-ask system instruction alone does not hold for this model. Fix in the harness: in a headless one-shot (frontend one-shot, HITL unavailable), when the final assistant message ends the turn without any write/edit tool call and reads as a question or menu, append one bounded continuation user message ('No human is available. Proceed with the approach the request names, or the first option you listed; make the change, verify it, and report.') and continue, up to 2 times; record an event for each. Deterministic tests on the turn loop with a fake provider (question -> continuation -> edit). Then the conductor reruns GLM live.

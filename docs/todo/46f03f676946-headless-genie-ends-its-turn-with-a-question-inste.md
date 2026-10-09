---
id: 46f03f676946
kind: bug
title: headless genie ends its turn with a question instead of acting, and loads no project or repository instructions (CLAUDE.md/AGENTS.md fragments are 0 bytes)
seq: 52
status: done
priority: p1
labels:
    - genie
created: 2026-10-09T14:37:47.699084Z
assignee: claude-sonnet5
sprint: 379
sprint_id: 908c2ac2-e7bc-55fe-86bd-d25046ac4684
sprint_title: 'bashy 1.0.0 feature list: bash + Bash# + Yoke'
closed: 2026-10-09T15:36:17.079911Z
closed_by: claude-opus5.5
---

Sprint 379 conductor 2026-10-09, weave yoke run 15 (genie, glm-5.3, session 7ddbb6d4-7ea7-4931-b69e-2ed241374244), after the compaction fix (ycode 9f6435a: 0 compactions this time, good). The one-shot brief (3297 bytes) was admitted in full and named the exact fix (Windows: stop the door with Process.Kill), yet after 104 read-only commands over 8 minutes the model ended the turn listing four options and asking 'Tell me which ... and I'll make the precise edit'; no file was edited. context.loaded shows fragments identity 2714 bytes, project-instructions 0 bytes, repository-instructions 0 bytes, although the workspace has CLAUDE.md (and AGENTS.md where present): genie loads none of the repo's instructions. Also: a read-only '[ -f <dynamic> ]' in a compound read command was denied by policy rule incomplete-preflight (intent contains unclassified or dynamic effects). Expected: (1) repository/project instructions load from AGENTS.md, then CLAUDE.md (the umbrella convention) with a test; (2) a headless one-shot (no human: frontend one-shot / HITL unavailable) instructs the model to act on the request and never end a turn with a question; if the request is ambiguous, choose the brief's stated approach or report a blocker; a test on the assembled prompt; (3) a read-only test builtin with a dynamic path classifies as read, not incomplete; red/green test. Live proof afterwards: re-run the yoke llm-down Windows story with genie glm-5.3.

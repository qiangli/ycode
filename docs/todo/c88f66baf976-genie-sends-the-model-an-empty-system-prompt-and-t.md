---
id: c88f66baf976
kind: bug
title: genie sends the model an empty system prompt and '[tool result omitted before model retry]' for every tool result under a 20480-token context budget, and a malformed tool call aborts the whole turn
seq: 53
status: done
priority: p1
labels:
    - genie
created: 2026-10-09T15:58:47.948933Z
assignee: claude-sonnet5
sprint: 379
sprint_id: 908c2ac2-e7bc-55fe-86bd-d25046ac4684
sprint_title: 'bashy 1.0.0 feature list: bash + Bash# + Yoke'
closed: 2026-10-09T17:43:25.879564Z
closed_by: claude-opus5.5
---

S379 2026-10-09 live (genie glm-5.3, weave yoke run 21, session 143263c0..., events copied to /tmp/genie-yoke21-events.jsonl; payload bodies under ~/.bashy/ycode/harness/payloads/<sha>): after the instruction-loading fix (project fragment 3513 bytes, 0 compactions) the model ran 206 commands in 12 min with no edit. The llm.requested payloads show System empty and every earlier tool_result content replaced by '[tool result omitted before model retry]'; context.measured reports context_budget 20480 for glm-5.3 (a model with a far larger window). The model therefore never sees the system prompt or its own command output and re-explores. The run then ended with llm.completed outcome protocol_error 'provider returned invalid bashy tool input' -> turn.failed, instead of returning a tool error to the model and continuing. Expected: (1) the context budget comes from the model's real window (registry/catalog), not a 20k default; (2) the system prompt is always sent (check the retry/truncation path); (3) tool results are kept or summarized, never blanket-omitted on a normal turn; (4) an invalid tool call becomes a tool_result error the model can correct (bounded retries), per the declared malformedToolResult: repair-explicitly. Red/green tests for each; then the conductor re-runs GLM live.

---
id: de2e126d0fae
kind: feature
title: Disable loop, context, and model tool calls
seq: 44
status: done
priority: p1
created: 2026-09-23T18:47:32.930962Z
assignee: codex-gpt5.6-sol
sprint: 264
sprint_id: 04832379-25cb-5254-83e6-12f82fa9617a
sprint_title: Ycode configuration absence and default semantics
closed: 2026-09-24T00:30:08.247275Z
closed_by: codex-gpt5.6-sol
---

Document and support the simple configuration path for the requested benchmark controls: context/memory behavior is off when its pipeline stages/resources are omitted or explicitly null; looping is off when the repeat stage is omitted/null; model tool availability follows model.capabilities.toolCalls, with false sending no Bashy tool definition. Do not add a profile selector, generic plugin model, or runtime-construction redesign.

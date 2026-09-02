---
id: d57c126b24d8
kind: task
title: Implement behavioral harness impersonation profiles
seq: 30
status: todo
priority: p0
created: 2026-09-02T20:53:52.822547Z
sprint: 106
---

Create complete Codex, OpenCode, OpenClaw, and Hermes agent.yaml profiles backed only by the extracted Bashy command/utility kit. Add differential golden-trace tests for context assembly, prompt/cache boundaries, memory and compaction, queue/steering, model calls, tool/HITL transitions, retries, subagents, lifecycle events, output, and restart/resume. An impersonation profile must run through the same neutral ycode DAG kernel; no product-specific Go branch is allowed. Document any intentionally unsupported wire/UI compatibility separately.

---
id: d57c126b24d8
kind: task
title: Implement behavioral harness impersonation profiles
seq: 30
status: done
priority: p0
created: 2026-09-02T20:53:52.822547Z
sprint: 106
---

Create complete Codex, OpenCode, OpenClaw, and Hermes agent.yaml profiles backed only by the extracted Bashy command/utility kit. Add differential golden-trace tests for context assembly, prompt/cache boundaries, memory and compaction, queue/steering, model calls, tool/HITL transitions, retries, subagents, lifecycle events, output, and restart/resume. An impersonation profile must run through the same neutral ycode DAG kernel; no product-specific Go branch is allowed. Document any intentionally unsupported wire/UI compatibility separately.

## Evidence

- Complete behavioral profiles live in `examples/harness-reconstructions/`; each
  compiles as an independent Harness, declares `bashy-run-v1`, embedded-only
  resolution, the portable Bashy command kit, and behavioral compatibility.
- `internal/harness/pipeline/reconstruction_test.go` executes all profiles through
  the neutral typed runner and deterministic mock provider, asserts that Bashy is
  the model's sole visible tool, compares canonical golden traces, and rejects
  product-specific Go branches in the harness kernel.
- The differential coverage gate accounts for context/cache, memory/compaction,
  steering, provider/tool/HITL, retry, delegation, lifecycle, output, and durable
  restart/resume. Resume reopens the event store and verifies sequence and digest
  continuity before approval resolution and completion.
- `docs/harness-impersonation-profiles.md` records the supported behavioral
  boundary and intentionally unsupported vendor wire/UI compatibility.
- Verification: `go test -race -count=3 ./internal/harness/pipeline
  ./internal/harness/event ./internal/harness/agent ./internal/harness/turn
  ./internal/harness/stages/hitl ./internal/harness/stages/memory`.

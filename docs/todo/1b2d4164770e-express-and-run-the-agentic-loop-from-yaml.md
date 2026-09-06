---
id: 1b2d4164770e
kind: task
title: Express and run the agentic loop from YAML
seq: 12
status: done
priority: p0
created: 2026-09-02T20:26:06.34657Z
sprint: 106
---

Wave 2; depends on provider, stages, events, Bashy and HITL. Run the complete starter turn pipeline without hardcoded retries, convergence, preactivation, cascade, stop rules or tool selection.

Evidence: `internal/harness/turn` registers the compiled starter graph stages and runs the strict `examples/agent.schema.yaml` turn through context, memory, the canonical mock-provider stream, checkpointing, loop convergence, and ordered output delivery. A restart test reopens every durable store and controller, then verifies checkpoint presence and continued event hash-chain integrity. `go test -race ./internal/harness/turn ./internal/harness/pipeline ./internal/harness/provider ./internal/harness/stages/ioctx ./internal/harness/stages/memory ./internal/harness/stages/hitl ./internal/harness/bashy` passes.

---
id: 10b93a78d320
kind: task
title: Replace the public Go embedding API
seq: 17
status: done
priority: p1
created: 2026-09-02T20:26:06.43867Z
sprint: 106
---

Wave 3; depends on compiler and turn pipeline. Expose Load, Validate, Run, Resume and streamed Event; update Go examples and remove compatibility signatures.

Evidence: `pkg/ycode` now exposes strict YAML-native `Load`, top-level and loaded `Validate`, `Run`, live checkpoint `Resume`, canonical streamed `Event`, and content-addressed `Payload`. The public runtime composes the neutral turn, durable event/payload stores, Memex, Bashy/HITL, provider normalization, and the compiled OTel observer. Resume unblocks the suspended graph at `hitl.review` without re-running admission, context, or prompt assembly. External compile-time API coverage and deterministic run/resume tests pass under `go test -race ./pkg/ycode/... ./internal/harness/turn ./internal/harness/bashy`.

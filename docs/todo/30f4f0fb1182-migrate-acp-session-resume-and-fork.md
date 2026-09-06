---
id: 30f4f0fb1182
kind: task
title: Migrate ACP session resume and fork
seq: 16
status: done
priority: p1
created: 2026-09-02T20:26:06.420118Z
sprint: 106
---

Wave 3; depends on events, roster and turn pipeline. Project ACP onto the same events and implement restart-safe session resume/fork with protocol conformance tests.

## Evidence

- ACP now delegates turns to the public YAML-native Harness and projects only canonical `output.emitted` payloads; lifecycle negotiation never constructs or starts a second agent loop.
- Session identity, turn allocation, close/resume, fork ancestry, boundary sequence and a hash-chained lineage log survive process restart in an atomically replaced private store.
- Public `Harness.Fork` validates the exact parent event, requires a completed turn checkpoint, emits one durable `session.forked` event, and snapshots the derived child boundary without invoking the provider/tool pipeline.
- The optional `coreutils/pkg/acp.SessionLifecycle` keeps existing runners source-compatible while strictly advertising and delegating v1 close/list/resume/fork capabilities.
- `go test -race ./pkg/ycode ./internal/harness/acp ./cmd/ycode -run 'TestHarness|TestACP|TestServeACP' -count=1`
- `go test -race ./pkg/acp`

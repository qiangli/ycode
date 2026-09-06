---
id: ce115e211851
kind: task
title: Build full lifecycle conformance and eval suite
seq: 20
status: done
priority: p0
created: 2026-09-02T20:26:06.493722Z
sprint: 106
---

Wave 4; depends on all frontends. Cover providers, golden event replay, Bashy outcomes, restart HITL, compaction, subagents, PTY, server, ACP, embedding and platform behavior.

## Evidence

- `scripts/harness-conformance.sh` is the single machine-runnable release gate.
  It repeatedly exercises the strict contract and five profile goldens; provider
  normalization; synchronous and durable Bashy outcomes; event replay; live and
  restart HITL; compaction; subagents; observability; local, HTTP, WebSocket and
  injected-NATS frontends; public embedding; utility commands; canonical ACP
  restart/fork; and a real pseudo-terminal REPL lifecycle driven by the neutral
  frontend adapter.
- `internal/harness/conformance/suite_test.go` makes every promised surface name
  an executable test owner, compiles every profile, requires every golden, and
  prevents the release gate from silently degrading into documentation-only
  coverage.
- Platform behavior is explicit: PTY conformance runs on Darwin and Linux.
  Native Windows ConPTY and undeclared operating-system PTY adapters are
  intentionally unsupported; all platforms retain strict
  `unsupportedEffect: reject` validation and all non-PTY frontend gates.
- Final verification passed on `darwin-arm64` with
  `YCODE_HARNESS_CONFORMANCE_COUNT=3 ./scripts/harness-conformance.sh`. The PTY
  subprocess gate runs once per invocation while all deterministic Go suites run
  three times under the race detector.

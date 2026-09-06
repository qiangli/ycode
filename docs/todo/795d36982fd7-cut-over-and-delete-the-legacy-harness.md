---
id: 795d36982fd7
kind: task
title: Cut over and delete the legacy harness
seq: 21
status: done
priority: p0
created: 2026-09-02T20:26:06.512345Z
sprint: 106
---

Wave 4; depends on green conformance. Route every entrypoint through agent.yaml; remove settings merge, policy-heavy conversation loop, specialized model tool registry and native/host fallback; update feature paths.

## Evidence

- `examples/agent.yaml` is the sole canonical self-hosting fixture. Root argv, stdin, prompt, REPL/TUI, HTTP, WebSocket, NATS, ACP, eval, Bashy shell and the public API all enter through the same compiled Harness boundary.
- The legacy settings merger, conversation/session loop, specialized tool registry, native shell/toolexec, direct Git process fallback, and reverse-only CLI/server/service/plugin adapters were physically removed.
- `internal/features/yaml_harness_cutover_test.go` scans the complete non-priorart production tree and the `cmd/ycode` plus `pkg/ycode` dependency closure for prohibited legacy paths and fallback symbols.
- `go list ./...`, `go test -short ./...`, `go test -short -race -count=1 ./...`, and `YCODE_HARNESS_CONFORMANCE_COUNT=1 ./scripts/harness-conformance.sh` pass after the cutover.

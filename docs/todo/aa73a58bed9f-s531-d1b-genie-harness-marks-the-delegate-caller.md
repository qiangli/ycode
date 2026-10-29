---
id: aa73a58bed9f
kind: feature
title: S531.D1b genie harness marks the delegate caller
seq: 55
status: todo
priority: p0
labels:
    - genie
created: 2026-10-29T07:45:09.149635Z
sprint: 531
sprint_id: f7bc2570-ca8e-59b0-8163-c3c51ba23610
sprint_title: 'Genie core harness: drive vendor agent CLIs as tools'
---

Part of S531 D1 (delegate guards landed in yoke cf06659d). Problem: genie's executor passes an allowlisted environment to every bashy command (examples/genie/agent.yaml bashy.execution.environment.names), so BASHY_DELEGATE_CALLER / BASHY_DELEGATE_ALLOW never reach `bashy delegate` run from genie: delegation from genie is then unrestricted and unscored.

Scope (ycode only):
- internal/harness/bashy/executor.go environment(): when the compiled allowlist declares BASHY_DELEGATE_CALLER, ALWAYS set it to "genie" (ValueRef harness://delegate-caller), ignoring any host value (a host env must not be able to clear or change it). BASHY_DELEGATE_ALLOW passes through from the host env only (no default => delegation denied from genie).
- examples/genie/agent.yaml (and agent-yolo.yaml if it has its own list): add BASHY_DELEGATE_CALLER and BASHY_DELEGATE_ALLOW to environment.names with a short comment.
Acceptance: executor_test.go red/green: declared CALLER is forced to genie even when host sets CALLER=other or empty; ALLOW passes through when set and is absent when unset; undeclared names unchanged. Any spec/compile test that pins the genie allowlist updated. `go test -short ./internal/harness/... ./examples/genie/...` green. Generic commit wording; no hostnames.

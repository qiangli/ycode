---
id: fe824e92dc23
kind: task
title: 'Y1 bashy.run harness-authored command stage: with:{script,timeoutMs,effects}, typed in: -> env/stdin JSON, stdout -> typed out:; preflight -> policy -> digest; fail closed'
seq: 37
status: assigned
priority: p0
created: 2026-09-13T01:31:42.184328Z
weave: 1
assignee: qiangli
sprint: 163
---

Goal: bashy.run — a harness-authored bashy call as a neutral graph stage, so YAML can run "bashy kb context --json" (or any command) at a named point of the loop with harness authority. This is the plugin mechanism; it carries no kb-specific Go.

- Node shape: run: {stage: bashy.run, with: {script: "...", timeoutMs: N, effects: [read]}, in: {<port>: <state path>...}, out: {<port>: <type>}}. Typed inputs are passed as YCODE_IN_<PORT> env (scalars) and a stdin JSON object (all ports); stdout is parsed into the declared out port type (JSON) or a string port.
- Same path as a model tool call: preflight -> policy.evaluate -> digest-bound authorization -> bashy.execute (internal/harness/turn/tools.go); the effects ceiling declared on the node is enforced by preflight (a write-effect script under a read ceiling is denied); non-zero exit fails closed; output limits from spec.bashy.execution apply.
- Events: bashy.run.requested / .completed with payload refs, same as llm.requested.
- No hidden defaults: missing script/timeout/effects is a compile error (internal/harness/spec).

Files: internal/harness/turn/{tools.go,turn.go (register)}, internal/harness/spec/{spec.go,schema.go}, examples/agent.yaml (one example node), docs/harness-capability-map.yaml, tests.
Gate: go test -short -race ./internal/harness/... ; a node running printf of its env under a read ceiling succeeds and its stdout lands in the typed port; a node writing a file under a read ceiling is denied; exit 1 fails the node; bashy dag build.
Depends on: nothing in coreutils (any command works); U1 for vocabulary.

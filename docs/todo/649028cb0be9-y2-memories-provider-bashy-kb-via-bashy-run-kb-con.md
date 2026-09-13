---
id: 649028cb0be9
kind: task
title: Y2 memories provider bashy-kb via bashy.run (kb context / kb note add --candidate); knowledge port with ring/form/ref provenance; harness stops opening memex
seq: 38
status: todo
priority: p1
created: 2026-09-13T01:31:42.207172Z
sprint: 163
---

Goal: ycode reaches kb through bashy.run; memex is no longer opened by the harness.

- memories.<name>.provider: bashy-kb (schema: memex removed as a provider value). memory.recall becomes a bashy.run of "bashy kb context --json" with the YAML memory policy passed as flags (--rings, --forms, --budget = recall.maxTokens, --k = recall.maxItems); memory.write becomes a bashy.run of "bashy kb note add --candidate --ring agent --episode <session>" once per turn (Persist stage), gated in YAML by write.everyTurns.
- Prompt port memory -> knowledge; blocks carry ring/form/ref for provenance in prompt.assembled (ioctx.PromptMessage gains Ring/Form/Ref, all optional).
- pkg/ycode/harness.go no longer calls memex.Open; stages/memory keeps Compact/Measure only (recall/write move to YAML nodes); pkg/memex stays in-tree until C8's migration has run on the live stores, then a follow-up deletes it.
- examples/agent.yaml: turn = context.load || session.load || bashy.run(kb context) -> prompt.assemble [context, knowledge, history, input] -> loop -> bashy.run(kb note add --candidate) || session.commit -> output.emit.

Files: internal/harness/spec, internal/harness/stages/{memory,ioctx}, internal/harness/turn/stages.go, pkg/ycode/harness.go, examples/agent.yaml, docs/architecture.md.
Gate: go test -short -race ./internal/harness/... ./pkg/ycode ./cmd/ycode; ycode validate --file examples/agent.yaml; no memex dir is created under the control root on Load; the stub bashy in tests returns a fixed kb context envelope and it appears as the knowledge port in prompt.assembled; scripts/harness-conformance.sh goldens refreshed only after reviewing the diff.
Depends on: Y1; C3's envelope (until then the test stub defines it).

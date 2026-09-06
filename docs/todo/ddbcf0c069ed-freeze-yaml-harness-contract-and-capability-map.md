---
id: ddbcf0c069ed
kind: task
title: Freeze YAML harness contract and capability map
seq: 3
status: done
priority: p0
created: 2026-09-02T20:26:06.185499Z
sprint: 106
---

Wave 0. Review and commit docs/sprint-yaml-native-harness.md and examples/agent.yaml; inventory every current entrypoint/tool/policy and record its new stage, Bashy command, frontend, or deletion. Gate: documentation examples parse and the map has no unowned live capability.

Evidence: `docs/harness-capability-map.yaml` owns the compiled stage catalog,
all run forms, canonical frontends, live ycode/Bashy/sh seams, and legacy
entrypoints/tool-policy surfaces. `TestFrozenCapabilityMapOwnsLiveSurface`
strictly decodes that ledger, verifies every source anchor, proves the sole
model-visible capability is Bashy, rejects forbidden host/tool-registry seams
in the neutral kernel, compares the ledger with the compiler's closed catalog,
and compiles `examples/agent.schema.yaml`. The focused spec race gate passes
twice.

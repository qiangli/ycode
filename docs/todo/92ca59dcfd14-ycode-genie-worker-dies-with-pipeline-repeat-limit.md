---
id: 92ca59dcfd14
kind: bug
title: 'ycode genie worker dies with ''pipeline: repeat limit 250 reached'' (bashy dag chat, exit 1) after ~24 min, no work committed'
seq: 64
status: todo
priority: p2
created: 2026-11-24T14:55:59.045425Z
---

2026-11-24 06:31-06:55 PST, dev box: Sprint 691 H6b assigned to ycode-glm-5.3 (weave run 236, instance ycode-cascade-x3). The tool exited 1 with 'error: pipeline: repeat limit 250 reached' from 'bashy dag --quiet --file dag.md chat' in the genie bar cache; duration 1424 s; the run had only a pin-bump commit. Classification pending (harness loop guard vs model looping); reproduce with the same binding on a small story before blaming the model. Workaround: story moved to another vendor.

---
id: 0361bdcfbaf5
kind: task
title: promote v0.3.1 once the windows QA lane goes green
seq: 1
status: todo
priority: p2
created: 2026-07-22T06:12:57.908028Z
---

v0.3.1-dev is published and refs/qa/v0.3.1/darwin exists (2026-07-22). promote.yml's default gate is 'windows', so promotion waits on refs/qa/v0.3.1/windows, which puppy's standing poller produces on its 15m cycle.

Once present, push a bare v0.3.1 tag — promote.yml's primary trigger — and it byte-promotes the tested -dev assets. Do not re-cut or rebuild: prod ships exactly the bytes QA ran.

Context: this is ycode's FIRST run through the two-stage pipeline. release.yml and promote.yml existed but had never been exercised (no -dev tag had ever been cut), and the first attempt surfaced two real breakages — a stale coreutils sibling pin that made main uncompilable in CI, and an untracked/misnamed QA harness. Both fixed. Worth confirming the whole chain completes once rather than assuming it now works.

Longer term, see the prod-poller task in outpost: it would create this bare tag automatically instead of by hand.

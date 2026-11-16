---
id: 2f6824bb1c31
kind: bug
title: TestRecall_TimeWindowFastPath fails on main (out-of-window memory ranks 1)
seq: 61
status: todo
priority: p2
labels:
    - test
created: 2026-11-16T11:53:52.199086Z
---

Pre-existing failure found 2026-11-16 (a Monday) during the bashy pin bump (umbrella todo 453d6c7a); reproduced on unchanged origin/main in a clean worktree, macOS dev box, GOWORK=off go test -short ./pkg/memex/memory. memory_time_test.go:91: out-of-window memory at rank 1 (keyword score 0.4487 vs this-week-b 0.4489). The package links none of the bumped modules. Suspects (unverified): a weekday/time-window boundary or near-tied keyword scores. Root cause not yet identified: bisect, classify and record before any fix.

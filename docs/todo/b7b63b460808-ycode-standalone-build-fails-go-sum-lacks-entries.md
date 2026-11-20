---
id: b7b63b460808
kind: bug
title: 'ycode standalone build fails: go.sum lacks entries after ''point shell consumers at separated repositories'' (e5be313)'
seq: 63
status: done
priority: p2
labels:
    - build
created: 2026-11-20T09:24:10.735556Z
assignee: claude-opus5.5-s676
sprint: 676
sprint_id: cc17f40d-d5ff-5d27-bbed-b8563759cde6
sprint_title: 'Fleet hardening after the tool-shortcut tests: steerable ycode/zcode/dsh + pre-existing test failures'
closed: 2026-11-20T16:01:37.760635Z
closed_by: claude-opus5.5-s676
---

2026-11-20: GOWORK=off go build ./cmd/ycode on clean ycode main e5be313 fails with 155 'missing go.sum entry' errors (github.com/qiangli/bash/interp, bashsharp/godelta, yoke/pkg/dag, coreutils/pkg/lockfile, ...); inside the umbrella workspace it builds. Consequence: bashy TestGenieRecipeGeneratesCallerWorkspaceAndModel (builds ../ycode) fails whenever bashy tests run standalone in the umbrella layout. Fix: complete ycode go.sum for its pinned requirements (GOWORK=off go mod tidy in ycode, check that no requirement it needs is dropped), then rerun the bashy genie test.

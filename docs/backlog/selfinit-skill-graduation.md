---
title: Graduate selfinit from experimental to stable
priority: p2
state: open
created: 2026-05-10T00:00:00Z
acceptance:
  - selfinit code paths no longer require -tags=experimental to compile
  - docs/strategy.md graduation criteria are checked off (test coverage, documented behavior, no open critical bugs)
  - docs/selfinit.md describes the stable contract, not the experimental one
  - A clean make build passes without the experimental tag set
gitea_issue: 4
---

## Context

`docs/selfinit.md` and `cmd/ycode/selfinit_hook.go` ship behind the
`experimental` build tag. The mechanism (auto-establishing ycode in
every repo on first invocation, hooking into `loom_lease` for foreign
tools) has been stable for several iterations and is depended on by
the lighthouse pattern.

## Why p2

It's working. Graduation is a hygiene task — flip the build tags,
prune the stub files, and update the docs. Not blocking new feature
work but keeps the strategy.md graduation discipline honest.

---
title: Investigate intermittent merger races in collab orchestrator
priority: p2
state: open
created: 2026-05-10T00:00:00Z
acceptance:
  - Reproduce the race condition flagged near internal/gitserver/collab/orchestrator.go:78-80 with a regression test (uses t.TempDir + the embedded Gitea fixture, no network)
  - Root-cause the failure mode (not just paper over with retries)
  - Fix passes -race -count=100 reliably
gitea_issue: 3
---

## Context

Comments around `internal/gitserver/collab/orchestrator.go:78-80`
flag intermittent merger races during multi-agent collab runs. The
orchestrator is now the dispatch layer for the Foreman/Worker split
(see `docs/backlog.md`), so any flake in this code path manifests as
Worker failures the Foreman has to retry.

## Why p2

Workers retry on failure, so the user-visible impact is a slower loop
rather than a stuck loop. But three consecutive failures abort the
Foreman per the skill's stop conditions, so flakes do bound the
autonomy window in practice.

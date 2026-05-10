---
title: Close coverage gaps in skills-dogfood telemetry
priority: p3
state: open
created: 2026-05-10T00:00:00Z
acceptance:
  - Tests exist for each phase skill (build/validate/deploy/eval/learn) covering the happy path
  - The end-of-week analysis queries in docs/skills-dogfood.md are wrapped behind `make dogfood-stats` (or equivalent) so the daily ritual is one command
  - At least one failure-mode test confirms err_kind values surface correctly in skill-usage.jsonl
gitea_issue: 5
---

## Context

`docs/skills-dogfood.md` defines a one-week structured run to validate
the external skill catalog. The infrastructure (telemetry hook +
process doc + friction log scaffold) is wired up but the analysis
queries are jq one-liners in prose, easy to skip. The phase skills
(build/validate/deploy/eval/learn) lack their own happy-path tests.

## Why p3

The dogfood week is the gate to round-1 catalog fixes. Tooling here
is amplification, not blocker — the week works without it. Bumps to
p2 if the friction of running the queries actually stops the user
from sticking with the dogfood discipline.

---
id: d16a4ff5691c
kind: bug
title: S557 repair recall time-window fast-path regression
seq: 65
status: assigned
priority: p1
created: 2026-11-02T00:45:58.641024Z
assignee: s557-manager
sprint: 557
sprint_id: f5ccf563-f5d1-5b94-8be4-9388e51ef86d
sprint_title: 'Bashy v1.0.0 preparation: split private/public development and retire qiangli/dhnt'
---

Native41359794189 macOSworkspace TestRecall_TimeWindowFastPath pkg/memex/memory fails; evidence/tmp/s557-native-125007548972.log1688. Diagnoseclockwindow/ordering contract beforechange; preserve recallfilters/ranking. Boundedtests nofullsuite/productbuildlocal. ExactYcode14ac873, old63ownermetadatauncommittedpreserve.


## Delivery

The fixture assumed that yesterday belongs to this week. That fails at Monday's boundary, matching the hosted run shortly after Monday 00:00 UTC. It also constructed UTC timestamps while Recall interprets relative weeks in the current local timezone.

The test now derives its two in-window timestamps from the same local reference time: now and the inclusive Monday start returned by `DetectTimeWindowAt`. The older memory remains one month earlier. Distinct memory names preserve two hits even when the timestamps coincide exactly at Monday midnight. No production clock hook, ranking weight, time filter, or policy changed.

Authored fixture guards cover all seven weekdays at start/end of day in UTC and fixed west/east offsets (42 cases), checking the exact known Monday boundary and excluding the older timestamp. The integration assertion is strengthened to require at least two results and both distinct in-window memories in the top two, while retaining the original time-source and no-old-memory-at-top assertions.

Validation: `GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2 go test -race ./pkg/memex/memory -run '^(TestRecall_TimeWindowFastPath|TestRecallWeekFixtureTimesEveryWeekday)$' -count=3 -timeout=60s` passed in 1.556s. Only this fixture and its story changed; unrelated story63 metadata was left untouched. Hosted native acceptance remains the manager's gate.

---
id: a724a4808b5f
kind: bug
title: S557 isolate native session controls and diagnose Windows latency gate
seq: 64
status: assigned
priority: p1
created: 2026-11-01T18:18:58.015758Z
assignee: s557-test-readiness
sprint: 557
sprint_id: f5ccf563-f5d1-5b94-8be4-9388e51ef86d
sprint_title: 'Bashy v1.0.0 preparation: split private/public development and retire qiangli/dhnt'
---

Full native41332670393 Windows workspace atYcode9411250 passes24otherchecks but ycode-unit fails: Competitive_LatencyComparison P99 20.0589ms vs10ms, YAMLCLISessionGroupAndResume emptylog succeeds unexpectedly, DeclaredSessionControlsExecuteAndDryRun sees priorpersistedsessions. Diagnose exactmechanisms, fix isolation/productdefects, retain sessioncontrols andlatencythreshold contracts unless a separatelyreviewed measurementcorrection proveswrongboundary. No skips or looseningthresholds. Boundedfocusedtests/crosscompile local; nativeWindows hosted. Existingstory63 metadata belongsmanager, do notstageit. Root reviews/pushes/pins.

## Delivery evidence

The Windows workspace job in umbrella run 41332670393 tested Ycode
9411250ceb14e51b7e0e52cad872b446b2e922f2. Its three failures were the two
session tests and the latency assertion described above; this is not a new
candidate acceptance result.

Session fixtures cleared BASHY_HOME and set HOME, but ControlBase delegates
home discovery to os.UserHomeDir, which uses USERPROFILE on Windows. Thus the
session tests read earlier persisted sessions rather than their intended empty
stores. The four tests in session_cmd_test.go and session_controls_test.go now
supply an explicit temporary BASHY_HOME. Production resolution, resume errors,
dry-run assertions, session pointers, and durable controls are unchanged.

Recall measures the complete operation, including rereading 60 files through
Store.List/Load. Load unnecessarily performed os.Stat even when both timestamps
were present in frontmatter. It now performs that fallback only when a timestamp
is absent. The new four-case regression checks both persisted timestamps, each
partially populated case, and the legacy case against a distinct file mtime.
There is no cache and external file edits remain visible on each recall.

The 10 ms P99 threshold and timing boundary are unchanged. Local baseline already
passed three repetitions (P99 9.526, 1.753, 2.742 ms); the optimized local run
passed at P99 2.741 ms. These measurements do not establish the cause of the
hosted Windows 20.059 ms outlier or prove it resolved. Native Windows execution
of the unchanged latency gate remains required before acceptance.

Bounded validation: four session tests passed (3.968 s); existing Store tests,
new timestamp cases, and unchanged latency gate passed (1.060 s). Full repository
suites remain hosted; no tests or thresholds were removed or weakened.

The same four session tests plus TestStoreLoadTimestampFallback also passed a
focused local race run. Native Windows validation remains pending.

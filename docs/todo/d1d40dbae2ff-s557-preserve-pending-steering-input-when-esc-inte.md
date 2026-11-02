---
id: d1d40dbae2ff
kind: bug
title: S557 preserve pending steering input when ESC interrupts the PTY turn
seq: 66
status: assigned
priority: p1
created: 2026-11-02T02:19:31.73807Z
assignee: s557-test-readiness
sprint: 557
sprint_id: f5ccf563-f5d1-5b94-8be4-9388e51ef86d
sprint_title: 'Bashy v1.0.0 preparation: split private/public development and retire qiangli/dhnt'
---

Frozen native41370643706 macOS workspace job125040178374 atYcode5518a65 failed TestTUIOverPTY (17.08s): line187 expected interrupted; running what was typed during it after slowturn+steering+ESC; marker never appeared. Preserve all PTY interactions, pending input semantics, followup execution, deadlines and assertions. Diagnose source and bounded focused authored test; no timeout widening or full local suite. Existing63/64 are Windows file/session/latency scopes and do not cover this macOS interactive cancellation failure. Parent owns integration and hosted verification.

## Delivery evidence

The test waited for the generic `esc stops` text already present from earlier
turns, so this did not establish that the new slow request had reached the stub
provider. The provider blocks only when its request contains `slow` without
`steer me`. The hosted transcript shows `stub-answer-7` before the ordinary
interruption message: steering was consumed before the intended blocking call,
leaving no pending steering input for the asserted follow-up path.

The fixture now emits a unique readiness marker from inside the provider's
blocking branch. The PTY driver waits for that marker before sending steering.
All previous interactions, ESC and follow-up assertions, and existing fifteen-
second waits remain intact. Production code and cancellation policy are unchanged.
The marker is opt-in for this PTY fixture; shared provider users are unchanged.

The original local test passed three repetitions (10.000s); this does not erase
the hosted scheduling failure or claim a local reproduction. The corrected test
passes three repetitions under the race detector (25.455s):
`GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2 go test -race ./pkg/ycodecli
-run '^TestTUIOverPTY$' -count=3 -timeout=90s`.
No full suite or product build was run. Hosted macOS validation remains required.

---
id: b258935b16d2
kind: bug
title: 'genie turn stalls before the model: harness re-decodes the whole shared events.jsonl on every 50ms control tick and every stage'
seq: 50
status: doing
priority: p0
labels:
    - genie
    - perf
created: 2026-10-08T21:19:15.4623Z
sprint: 379
sprint_id: 908c2ac2-e7bc-55fe-86bd-d25046ac4684
sprint_title: 'bashy 1.0.0 feature list: bash + Bash# + Yoke'
---

Found by the S379 conductor on Dragon 2026-10-08: 'bashy genie -m door-codex-gpt-5.5 ...' produced no output for 7 min (timeout), no model request was made. ~/.bashy/ycode/harness/sessions/events.jsonl is 48 MB (all sessions, one log). A sample of the ycode process is dominated by event.Replay -> event.Decode -> json Unmarshal + eventDigest sha256, called from Harness.monitorControls (pkg/ycode/control_transport.go:146, a 50ms ticker that replays the entire log each tick) - and event.Replay(h.eventPath) is also called per stage (internal/harness/turn/stages.go:79, controls.go:21) and in pkg/ycode harness.go/sessions.go/controls.go/queue.go. Cost is O(log size) per tick, so genie degrades to unusable as history grows. Fix (KISS): an incremental reader on the event store - read from a remembered byte offset with the last verified sequence+digest, decode and verify only new lines (keep the full gap/digest validation for the new tail); use it in monitorControls and the per-stage reads; full Replay stays for Open/resume/list. Red/green: a test with a large synthetic log (e.g. 50k events) asserting monitorControls/stage reads do not re-decode old events (count Decode calls or bytes read) and that a control request appended later is still seen; existing harness tests stay green. Then rerun the live probe: in a fresh scratch git repo, bashy genie -m door-codex-gpt-5.5 'Run exactly this one shell command: pwd; echo steward-ok > f.txt; cat f.txt' must finish and create f.txt.

Worker rules (Sprint 379 conductor, 2026-10-08): work in the submodule named above; commit and push INSIDE it to its default branch (public repos: main), then stop - the conductor bumps the umbrella pin. Every commit carries Sprint: #379 plus Story/Story-ID trailers for this story, with BASHY_AGENT set to your own binding. Reproduce first with a red unit test, fix at the root, never skip, quarantine or add to a known-failures list. This dev box (Dragon) is for builds and focused unit tests only. Windows runs go to noviwin1.local, Linux runs to the novidesign.local podman machine bashy or the repo CI, and macOS full suites to novidesign.local (all passwordless ssh; use the .local names). Gate before push: go build ./... and go vet on the module, plus the focused tests, with the exit code captured (never gate a push on a pipe). Report: the root cause, the commit SHA, the exact tests run and where, and any CI run URL.

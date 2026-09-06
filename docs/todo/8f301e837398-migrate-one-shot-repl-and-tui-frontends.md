---
id: 8f301e837398
kind: task
title: Migrate one-shot REPL and TUI frontends
seq: 14
status: done
priority: p1
created: 2026-09-02T20:26:06.383203Z
sprint: 106
---

Wave 3; depends on turn pipeline and HITL. Submit canonical inputs and render one event stream for one-shot, stdin, REPL and TUI, including streaming, cancellation and approval resume.

Evidence: `internal/harness/frontend` exposes typed one-shot, bounded stdin,
line-oriented REPL, and presentation-only TUI adapters over the same compiled
`Controller`/`Local` seam. All four preserve caller-supplied canonical request
metadata and render the identical durable event stream; cancellation propagates
through context without a frontend policy branch, and REPL/TUI approval resume
uses the controller's single `Resume` stream. Parity, bounds, streaming
cancellation and resume tests pass five consecutive race-enabled runs.

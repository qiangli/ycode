---
id: 46d26643030a
kind: task
title: Migrate HTTP WebSocket and NATS frontends
seq: 15
status: done
priority: p1
created: 2026-09-02T20:26:06.401127Z
sprint: 106
---

Wave 3; depends on event store and turn pipeline. Drive serve transports through canonical input/events with YAML-configured auth, address and workspace selection.

Evidence: `internal/harness/frontend.Network` requires compiled authentication,
listen address or NATS endpoint/subject, applies bounded strict request decoding,
and dispatches HTTP, WebSocket and NATS onto the same canonical controller
input/event types. HTTP/WebSocket streaming and NATS collection honor request
cancellation; WebSocket frames are bounded and same-origin checked. The
injected NATS subscription adapter verifies the live connection endpoint,
subscribes only to the compiled subject, returns canonical events, and
unsubscribes on cancellation, with a concrete `nats.go` connection wrapper.
Parity, auth, address/subject, workspace-override rejection, streaming
cancellation and subscription lifecycle tests pass five consecutive race runs.

# AG-UI compatibility assessment

Status: research note and future architecture guidance. No AG-UI support is
implemented or claimed.

## Finding

The umbrella has considered much of the event architecture needed for an
agent-facing UI, but neither `ycode` nor `bashy` currently names or implements
the [AG-UI protocol](https://github.com/ag-ui-protocol/ag-ui).

AG-UI is an MIT-licensed, transport-independent protocol for connecting agent
backends to user-facing applications. It defines a small set of lifecycle,
message, tool, activity, state, and human-interaction events and can be carried
over SSE, WebSockets, or other transports. It complements MCP (agent-to-tool)
and A2A (agent-to-agent); it does not replace either.

## Existing local foundations

Ycode already has the closest internal surface:

- `internal/client` exposes event subscriptions through in-process, WebSocket,
  and NATS clients.
- The client/server design includes `turn.start`, `text.delta`,
  `thinking.delta`, `tool_use.start`, `tool.progress`, `tool.result`,
  `turn.complete`, `turn.error`, permission request/response, usage updates,
  session updates, message submission, and cancellation.
- `internal/wireevents` gives the first-party noninteractive harness a smaller
  `turn.start` / `tool.call` / `turn.end` stream.
- The conversation runtime already owns model streaming, tool dispatch,
  permissions, cancellation, and session state—the facts an AG-UI adapter
  would need to publish.

Bashy supplies lower-level orchestration and process facts. Its PTY protocol
distinguishes typed text from verbatim keystrokes, and tools declaring an
`events_arg` can report an authoritative `turn.end`. These are useful adapter
inputs, but Bashy's current stream is not a frontend interaction protocol.

## Provisional event mapping

| Local event | AG-UI concept |
|---|---|
| `turn.start` | run started |
| `text.delta` | text-message content |
| `tool_use.start` | tool-call start |
| `tool.progress` | activity snapshot/delta or custom event |
| `tool.result` | tool-call result |
| `turn.complete` / `turn.end` | run finished |
| `turn.error` | run error |
| `session.update` | state snapshot/delta |
| permission request/response | human-in-the-loop custom/extension events |
| message submission / cancellation | run input / cancellation control |

This table is a design aid, not a wire contract. Correct adaptation requires
AG-UI lifecycle pairing, stable thread/run/message/tool-call identifiers,
ordering rules, reconnect behavior, and explicit treatment of state snapshots
versus deltas.

## Gaps before compatibility can be claimed

- No AG-UI `RunAgentInput` request boundary or advertised media type.
- No tested mapping to the complete AG-UI event set.
- No stable cross-surface identifier contract for threads, runs, messages,
  activities, and tool calls.
- No AG-UI state snapshot/delta or frontend-tool contract.
- No AG-UI HTTP/SSE endpoint, middleware, resume, or replay behavior.
- Bashy's compact wire events and ycode's richer client events are not yet one
  canonical transport-neutral schema.
- No protocol-version pin, compatibility tests, or malformed-sequence tests.

## Ownership recommendation

AG-UI belongs primarily at the **Tessaro/ycode frontend boundary**:

1. Ycode owns the canonical agent lifecycle, message, tool, permission, and
   state events and the AG-UI adapter.
2. Tessaro exposes the adapter to browser/cloud clients over HTTP/SSE or
   WebSockets.
3. Bashy continues to provide reliable process, orchestration, and first-party
   harness events without importing frontend protocol concerns throughout the
   shell and command core.
4. A later `bashy agent serve --protocol ag-ui` may be a thin composition
   surface, not a second implementation.

## Adoption plan

### Phase 0 — compatibility guardrails

- Pin an AG-UI release/schema for evaluation; do not code against moving main.
- Add stable IDs and lifecycle invariants to new local event work.
- Avoid adding local events that cannot be losslessly represented as a typed
  lifecycle, message, tool, activity, state, or custom event.

### Phase 1 — canonical local event model

- Reconcile `internal/wireevents` with the richer client/server bus.
- Keep transports outside the event model.
- Test ordering, exactly-once terminal events, cancellation, tool-call pairing,
  permission round trips, and reconnect/replay.

### Phase 2 — ycode AG-UI adapter

- Implement a narrow mapper around the canonical model, preferably behind an
  internal interface before choosing an SDK dependency.
- Add protocol fixtures for text streaming, tool calls, errors, cancellation,
  shared state, frontend tools, and human approval.
- Verify unknown/custom event preservation and malformed lifecycle rejection.

### Phase 3 — Tessaro endpoint

- Publish the adapter through an authenticated HTTP/SSE endpoint first;
  WebSocket support should reuse the same event mapper.
- Exercise reconnect, backpressure, authorization, tenant/session isolation,
  and audit logging end to end.

### Phase 4 — optional Bashy surface

- Add a thin CLI/server entry point only if local desktop or self-hosted users
  need AG-UI without Tessaro.
- Keep AG-UI dependencies out of the POSIX shell and core orchestration paths.

## Timing recommendation

Do not interrupt the active POSIX and utility-conformance campaigns for this.
Record it now, consolidate the event model after those delivery gates, and add
the adapter before committing to a substantial Tessaro web-agent UI. The
protocol is valuable chiefly because adopting it at that boundary avoids
inventing a private frontend wire format.


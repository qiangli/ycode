# ycode architecture

ycode is a compiler and runtime for a strict agent-harness language. The
authoritative input is `agent.yaml`; presentation layers do not own agent-loop
policy.

## One source of control

`internal/harness/spec` parses YAML with unknown-field rejection, validates
references and platform support, type-checks pipelines, detects cycles, and
computes the configuration digest. A valid document explicitly declares:

- runtime roots and control storage;
- immutable prompt/content sources;
- providers, models, routes, retries, quotas, and budgets;
- the sole Bashy tool contract and execution ceilings;
- contexts, memories, queues, sessions, lifecycle transitions, and locks;
- policies and HITL resume actions;
- agents, subagent relationships, hooks, and typed pipelines;
- local/network frontends, triggers, output routes, and observability.
- the command tree, arguments, flags, help, completion and typed CLI dispatch.

There is no secondary mutable configuration layer, no imperative model/tool
activation, and no hidden retry, convergence, or fallback policy. If a choice
changes behavior, it belongs in the compiled document.

`spec.interfaces.cli` projects this document onto a generic Cobra parser in
`internal/harness/cli`. The bootstrap discovers the selected YAML document;
neutral operation handlers dispatch the resulting typed invocation to the
existing composition root. Offline help embeds the authored canonical YAML,
and configured execution still loads the selected project file. See
[the CLI contract](cli-contract.md).

## Runtime flow

```text
configured frontend / Go API
          │
          ▼
input admission ── identity, size, schema, idempotency
          │
          ▼
typed pipeline runner ── needs DAG, typed ports/state, hooks, repeat bounds
          │
          ├── session history load (`session.load`, projection from the last committed turn)
          ├── context load + ordered prompt assembly
          ├── knowledge recall via bashy.run (`bashy kb context --json`)
          ├── configured context measure/compact
          ├── normalized provider stream
          ├── policy → Bashy preflight → allow/deny/ask → execution
          └── turn-boundary commit (`session.commit`)
          │
          ▼
output routing ── canonical event + content-addressed payload
```

Every stage receives explicit inputs and writes declared outputs. The neutral
runner in `internal/harness/pipeline` schedules nodes only from `needs`, checks
port types and writer rules, evaluates compiled expressions, applies declared
hooks, and enforces configured repetition. Stage implementations cannot add
ordering or policy.

`internal/harness/turn` registers the concrete stages and starts the selected
agent pipeline. Agent roster and delegation live in `internal/harness/agent`;
provider adapters, Bashy, memory, HITL, and I/O remain independent mechanisms.

## Canonical messages and providers

`internal/harness/message` defines the neutral conversation value.
`internal/harness/provider` normalizes Anthropic, OpenAI-compatible, Gemini,
and deterministic mock backends into the same message/event stream. The
adapter preserves streaming order, deterministic call IDs, usage, stop reason,
and normalized failures. It cannot inject tools or policy.

Models see exactly one function tool named `bashy`. Provider-specific wire
formats are an edge concern; the turn pipeline handles one canonical tool-call
shape.

## Bashy boundary

`internal/harness/bashy` adapts compiled YAML to Bashy's `bashy-run-v1`
contract. Preflight and execution bind the same script, cwd, environment,
limits, placement metadata, and intent digest. The result preserves stdout,
stderr, spill references, exit code, signal, duration, and structured outcome.

Policy evaluation happens in ycode, from the selected compiled policy. A call
cannot execute without complete preflight evidence and a digest-bound grant.
No host-shell fallback or provider-added tool is available.

## HITL and continuation

`internal/harness/stages/hitl` implements `policy.evaluate` and explicit
allow/deny/ask routing. Ask persists the pending decision and checkpoint before
emitting `hitl.waiting`. Resume uses decision version, review/report digests,
actor, and a one-use compare-and-swap transition:

- `approve` binds the reviewed intent;
- `edit` creates a new call and requires a new preflight;
- `reject` returns a typed rejection to the suspended pipeline.

Duplicate, stale, or mismatched resolutions fail closed. If the configured
frontend has no available human, the compiled unavailable-human action is
used; it is never inferred.

The public `Harness.Resume` and configured resume-capable frontends continue a
live suspended Go stack. A process restart restores durable session identity
and completed turn boundaries, but does not pretend to reconstruct an
interrupted stack. ACP durable lifecycle operations resume later turns from
those completed boundaries.

## Events, payloads, checkpoints, and fork

`internal/harness/event` owns the append-only JSONL record. Events contain a
schema version, global sequence, session/run/stage identity, config digest,
causation/correlation metadata, payload digest, previous digest, and event
digest. Each append is synchronized before it is streamed.

Large or model-visible content is stored in the content-addressed payload
store. Events carry references, making replay complete without duplicating or
silently truncating bytes.

Successful turns save a boundary checkpoint tied to the event position and
typed output. `Harness.Fork` validates the requested parent event and completed
checkpoint, appends one `session.forked` event, and saves a child checkpoint.
Fork is a lifecycle operation; it never invokes the provider or tool pipeline.
The forked event carries the parent's committed message reference, which
`session.load` uses as the child's history seed — the child's first turn
continues the parent's conversation.

## Frontends

`internal/harness/frontend` projects transports onto the same controller and
canonical events:

- local one-shot, stdin, REPL, and TUI;
- HTTP and WebSocket;
- NATS through an injected connection;
- ACP through `coreutils/pkg/acp`.

Frontends are usable only when declared in YAML. Admission limits,
authentication reference, HITL/resume support, routing, and output delivery
come from that declaration. A protocol adapter must not start a second agent
loop or translate success from presentation text.

## Knowledge and context

`internal/harness/stages/ioctx` snapshots bounded sources and assembles prompt
fragments in declared order with typed ports (`context`, `knowledge`,
`history`, `input`). Knowledge-port messages keep per-block `ring`/`form`/`ref`
provenance from the kb envelope, and `prompt.assembled` records it. Missing
required content, overflow, and delivery failure follow compiled behavior.

`internal/harness/stages/session` gives a durable session conversational
continuity. `session.load` projects the message history recorded at the last
`session.turn-committed` event (or a fork's parent seed) through the compiled
history policy — whole-turn trimming to `maxTokens`, tool-result clearing by
age, tool-pair repair — into the `history` port, and emits
`session.history.loaded`. `session.commit` writes the finished turn's message
array to the payload store at the turn boundary and emits
`session.turn-committed`. Two runs on one session id are one conversation,
across process restarts and forks; no frontend keeps a private transcript.

Memory is policy over the `bashy kb` front door (`provider: bashy-kb` is the
only compiled provider; the harness opens no store of its own). Recall is a
harness-authored `bashy.run` node running `bashy kb context --json` with the
compiled policy substituted as flags (`--rings`, `--forms`, `--budget` =
`recall.maxTokens`, `--k` = `recall.maxItems`); persist is a `bashy.run` node
running `bashy kb note add --candidate --ring agent --episode <session>` once
per turn (`write.everyTurns: 1`). Script templates substitute typed inputs and
the session id as single-quoted literals at stage time, before preflight, so
the digest-bound authorization covers the composed command; shell variable
expansion is never used because the intent analyzer cannot prove it.

`internal/harness/stages/memory` keeps the measure and compaction mechanisms
and decodes the frozen kb context envelope
(`testdata/kb-context-envelope.json`, byte-identical to the umbrella golden).
`context.measure` reports the last provider token count plus an estimated
tail, with provenance in `context.measured`; `memory.compact` produces a
user-role `[compaction-summary]` message through the compiled route and
prompt sources, with a deterministic fallback when so compiled. Budgets,
triggers, routes, and failure behavior come from YAML. There is no
background memory scheduler hidden outside the graph. See
[memory.md](memory.md) for the full stage model.

## Observability and security

`internal/harness/observe` attaches OpenTelemetry spans through the pipeline
observer only when compiled observability enables them and the embedding
supplies a tracer. Events remain the durable execution record; telemetry is
not used as state.

The runtime resolves workspace/read/write roots before execution, rejects root
overlap according to the compiled rule, redacts secrets and control paths from
events, stores private state with restrictive permissions, and validates
event/checkpoint integrity during replay. Unsupported platform or effect
combinations fail during compilation or admission.

## Package map

| Package | Responsibility |
|---|---|
| `internal/harness/spec` | strict compiler and capability validation |
| `internal/harness/pipeline` | typed DAG/state runner |
| `internal/harness/turn` | stage registration and complete agent turn |
| `internal/harness/provider` | canonical provider stream adapters |
| `internal/harness/bashy` | preflight/execution/control boundary |
| `internal/harness/stages/ioctx` | admission, content, prompt, output |
| `internal/harness/stages/session` | durable session history load and turn commit |
| `internal/harness/stages/memory` | context measure/compaction and kb envelope decoding |
| `internal/harness/stages/hitl` | policy decisions and durable review |
| `internal/harness/event` | event log, payload store, checkpoints |
| `internal/harness/frontend` | local and network transport projections |
| `internal/harness/acp` | durable ACP session identity and lineage |
| `internal/harness/observe` | optional compiled OTel observer |
| `pkg/ycode` | public Validate/Load/Run/Resume/Fork/Payload/Close API |

The canonical executable example is [`examples/agent.yaml`](../examples/agent.yaml).

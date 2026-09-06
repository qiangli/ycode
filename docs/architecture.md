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

There is no secondary mutable configuration layer, no imperative model/tool
activation, and no hidden retry, convergence, or fallback policy. If a choice
changes behavior, it belongs in the compiled document.

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
          ├── context load + ordered prompt assembly
          ├── configured memory recall/measure/compact/write
          ├── normalized provider stream
          └── policy → Bashy preflight → allow/deny/ask → execution
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

## Memory and context

`internal/harness/stages/ioctx` snapshots bounded sources and assembles prompt
fragments in declared order with typed ports. Missing required content,
overflow, and delivery failure follow compiled behavior.

`internal/harness/stages/memory` adapts `pkg/memex` behind explicit recall,
write, measure, and compaction stages. Scopes, ranking, budgets, triggers,
routes, and failure behavior come from YAML. There is no background memory
scheduler hidden outside the graph.

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
| `internal/harness/stages/memory` | explicit memory and compaction stages |
| `internal/harness/stages/hitl` | policy decisions and durable review |
| `internal/harness/event` | event log, payload store, checkpoints |
| `internal/harness/frontend` | local and network transport projections |
| `internal/harness/acp` | durable ACP session identity and lineage |
| `internal/harness/observe` | optional compiled OTel observer |
| `pkg/ycode` | public Validate/Load/Run/Resume/Fork/Payload/Close API |

The canonical executable example is [`examples/agent.yaml`](../examples/agent.yaml).

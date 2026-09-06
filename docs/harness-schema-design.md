# Harness Schema Design of Record

Status: Frozen Sprint 106 `ycode.dev/v1alpha1` contract. This document and
`examples/agent.yaml` are normative; changes require an explicit
versioned contract decision. `docs/harness-capability-map.yaml` binds every live
kernel capability and migration surface to its owner.

## Decision

Use one strict `apiVersion: ycode.dev/v1alpha1`, `kind: Harness` document with a
Kubernetes-shaped `spec`. The document is a closed, typed resource graph, not a
bag of settings and not a prompt template. Named resources are declared once and
referenced with fields ending in `Ref` or `Refs`.

The model-visible tool set is an invariant, not an extensibility point. The
compiler always emits exactly one tool named `bashy`, configured by the
`spec.bashy` singleton. Models, agents, memory, policies, queues, hooks,
placement and frontends are harness resources; they are not model tools. An
agent selects resources by typed reference and does not inherit ambient process
configuration.

## What the predecessor resource design contributes

The predecessor lq-ai agent-resource corpus supplied for migration was reviewed
as a read-only input. Its `claw`, `opencode`, `ralph`, and `shell` declarations
demonstrate that YAML can usefully describe model sets, reusable agent packs,
JSON-Schema parameters, instruction fragments, hooks and agents callable by
other agents. In particular, `claw` contributes context/worker decomposition,
`ralph` contributes bounded loop and fallback requirements, `opencode`
contributes a placement-aware foreign-command use case, and `shell` confirms
the value of one portable script boundary. Keep those concepts, but make every
composition edge explicit, typed, bounded, and deterministic.

Do not preserve these accidental contracts:

- package-wide defaults silently merged into individual agents;
- `kit:*`, `agent:*` and other wildcard tool grants;
- secrets stored in ordinary scalar fields;
- snake_case/camelCase mixtures and overloaded `model` strings;
- orchestration algorithms embedded only in instruction prose;
- executable Go or arbitrary tool implementations supplied by catalog YAML;
- map iteration determining tool order or conflict resolution;
- fallback from the embedded shell to an undeclared host command.

Reusable documents may be added through `spec.imports`. Each import declares an
immutable content digest, a namespace and selected exports. Import order has no
override meaning, name collisions are errors, and imports cannot contribute
runtime defaults. Compilation resolves imports into one closed graph and records
the digest of that graph.

## Top-level shape

Only `apiVersion`, `kind`, `metadata` and `spec` are accepted. `spec` contains
these closed resources:

| Resource | Responsibility |
|---|---|
| `runtime` | Entrypoint selection, trusted state and filesystem boundaries |
| `imports` | Pinned, namespaced reusable graph fragments |
| `sources` | Reusable literal/file content with explicit bounds |
| `providers` | Wire protocol and secret credential references |
| `models` | One concrete provider/model capability |
| `routes` | Ordered attempts, quota behavior and fallback |
| `bashy` | The sole portable model tool and its maximum authority |
| `contexts` | Ordered prompt fragments and budgets |
| `memories` | Recall, write and compaction mechanisms |
| `queues` | Durable admission ordering and capacity |
| `sessions` | Event log, content store, branches and retention |
| `policies` | Effect-based allow/ask/deny rules |
| `placements` | Local, remote or worktree execution constraints |
| `locks` | Named concurrency and mutation locks |
| `hooks` | Typed pipelines explicitly invoked at named boundaries |
| `skills` | Pinned content and reusable subgraphs |
| `pipelines` | Typed dataflow DAGs and bounded control flow |
| `agents` | Named identities selecting resources and a pipeline |
| `frontends` | Adapters onto canonical input and events |
| `triggers` / `sinks` | Authenticated admission and delivery routes |
| `observability` | Telemetry export and content-recording policy |

All maps reject an empty key. Resource names follow
`[a-z][a-z0-9]*(?:[-.][a-z0-9]+)*`. Map declaration order has no behavioral
meaning. Lists are ordered unless the field explicitly declares otherwise.

Reachability begins at enabled triggers and `runtime.defaultAgentRef`. An
otherwise unreachable reusable resource must declare `exported: true`; all
other unreachable resources are compiler errors.

## References, values and trusted storage

References are never inferred from names in another namespace. A field named
`modelRouteRef` resolves only under `spec.routes`; `memoryRef` resolves only
under `spec.memories`. Missing, type-confused and cyclic references are compiler
errors.

Environment access is structured:

```yaml
apiKey:
  secretRef: {provider: env, name: OPENAI_API_KEY}
```

Secret references are resolved only at use time. Resolved values must not be
stored in the compiled document, dumps, events, checkpoints, errors or spans.
Non-secret environment input uses `valueFrom`; `${...}` interpolation is not
part of the canonical format.

Content is also structured and mutually exclusive:

```yaml
sources:
  project-instructions:
    file: {path: AGENTS.md, required: false}
    limits: {maxBytes: 65536}
  identity:
    text: "You are a software-engineering agent."
    limits: {maxBytes: 4096}
```

Relative source files resolve from the harness document. After symlink
evaluation the path must remain within `runtime.readableRoots`. Each source is
snapshotted before use and identified by digest; later workspace mutation cannot
change the input of an in-flight or replayed run. Content cannot introduce YAML
configuration. Every source has a byte bound; pipeline stages additionally
apply token budgets.

`runtime.controlRoot` is trusted harness state. It must not overlap a readable
or writable Bashy root, placement mount, source root or imported path. The
runtime creates it without following symlinks, with private directory/file
permissions, atomic append and a single-writer lease. Events, checkpoints,
approvals, content-addressed payloads, queue leases and lock fencing records live
there. A model script can never read or mutate it.

## Model separation

A provider is a transport, a model is one concrete capability, and a route is
runtime selection policy. Keeping these separate prevents a model alias from
silently changing retry, credential or fallback behavior.

- `providers`: protocol, endpoint, headers and credential references.
- `models`: `providerRef`, exact model identifier, context/output limits and
  declared capabilities.
- `routes`: ordered model attempts, per-attempt timeout, retry classification,
  quota-pool policy and total spend/token bounds.

An agent references a route, never a bare provider and never an ambiguous
`provider/model` string. Provider adapters consume and emit one normalized,
versioned message/tool-call/outcome family. Call IDs are deterministic within a
run. Duplicate filtering, structured-output validation or repair, provider
session scope, streaming retry and route fallback are explicit stages or route
fields; an adapter may not silently select them.

Transport retry and route fallback are separate bounded concepts. A route item
may retry a failed transport attempt; fallback advances to the next ordered
route item. A pipeline retry around `llm.call` is rejected unless the route
declares that its own retry is disabled, preventing accidental multiplication.

## The Bashy boundary

`spec.bashy` configures the one `bashy` model tool. Its input and result schemas
are versioned contracts. It declares the maximum cwd roots, environment
allowlist, timeout, output bounds, parallelism, permission/effect ceiling and
required preflight. Agent, parent-delegation, policy and placement authority can
only attenuate this maximum; effective authority is their intersection.

The portable request supports bounded foreground execution and durable job
operations (`start`, `poll`, `signal`, `cancel`) under the same tool name and
result family. Job handles, placement affinity and partial output are durable.
Nonzero exit, signal and timeout are observations, not transport failures.

The model cannot add tools through agent YAML. Bashy commands remain governed by
the Command Atlas and the selected policy. The embedded resolver uses only
embedded builtins and declared, digest-pinned executables. Inheriting `PATH`
does not authorize host execution, and no missing command falls back to the
host. Configuring a specialized operation means adding a governed Bashy command
or script, not another function definition in the model request.

Preflight produces a closed report with commands, canonical paths, destinations,
credentials, effects, unsupported effects and completeness. Unknown,
unclassified or incomplete effects fail closed. Execution accepts an opaque
authorization binding, not a bare `allow` value. The binding covers script
bytes, resolved cwd, environment names and value digests, limits, placement,
policy/config versions and the complete preflight report. Execution rejects any
drift.

## Agents and authority

An agent is a named identity, not a nested copy of the runtime. It declares:

- JSON-Schema `inputSchema` and `outputSchema`;
- one `contextRef`, `modelRouteRef`, `pipelineRef`, `memoryRef`, `queueRef`,
  `policyRef`, `placementRef` and `sessionRef`;
- explicit delegation grants with agent, model-route, depth, concurrency,
  timeout, budget, placement and permission/effect ceilings;
- ordered hook references and presentation metadata.

There is no wildcard grant and no property inheritance. Contexts alone own the
ordered instruction/source list. `runtime.defaultAgentRef` chooses an agent;
that agent alone chooses its entry pipeline. A delegation is a normal
`agent.invoke` stage and produces the same canonical events as its parent.
Child authority is the intersection of its declaration and its delegation
grant; escalation is a compiler error. Session branch, join, cancellation and
handoff policy are declared per grant.

## Pipelines, state, expressions and outcomes

A pipeline is a DAG of named nodes. `needs` is the only dependency relation;
list order is only a deterministic scheduler tie-breaker. Pipeline `inputs`,
`state` slots and `outputs` carry versioned type names. State slots declare
whether they are immutable, single-writer or loop-carried and, for collections,
their deterministic merge order. Each stage exposes typed ports and binds them
explicitly with `in` and `out`; the compiler rejects a read that no predecessor
can produce on every reachable branch.

Each node selects exactly one `run` form:

- `stage`: invoke a built-in, policy-free mechanism;
- `pipelineRef`: invoke a named reusable subgraph;
- `repeat`: invoke a named subgraph with explicitly declared loop-carried state;
- `forEach`: invoke a named subgraph over a bounded collection;
- `switch`: select one named subgraph by ordered expressions;
- `fallback`: try bounded named subgraphs for declared outcome classes.
- `hook.invoke`: invoke one declared, typed hook pipeline.

Inline nested stage lists are excluded. Named subgraphs make reuse, validation,
event attribution and call-cycle detection unambiguous. Ordinary pipeline calls
must be acyclic. Only `repeat` creates a cycle, and it always declares a finite
iteration bound.

Conditions use one closed expression AST. Every operator maps to a list of
operands, for example:

```yaml
when:
  all:
    - {exists: [{field: state.response}]}
    - {eq: [{field: state.finished}, {literal: false}]}
```

Initial operators are `eq`, `ne`, `lt`, `lte`, `gt`, `gte`, `exists`, `in`,
`all`, `any` and `not`. A field operand is a typed state path and a literal
operand is YAML data. Expressions cannot call functions, read environment or
files, or mutate state. Loop and fan-out variables have lexical scope and cannot
escape except through declared outputs.

Every node produces a typed `node.outcome/v1` containing class, code, retryable,
observations and optional typed error detail. Skips have a distinct outcome and
cannot fabricate required outputs. Retry, fallback and failure routing match
this outcome explicitly. `failFast` is only scheduler cancellation behavior; it
is not failure routing.

Every repeat, retry, fallback and fan-out is bounded. There is no implicit stage
invocation, tool retry, provider fallback, queue drain, checkpoint, compaction,
approval, lifecycle transition or convergence rule in Go. Those actions appear
as nodes in YAML. Go may enforce an invariant by refusing unsafe execution; it
may not schedule an omitted policy action.

Hooks are typed named pipelines invoked through an explicit `hook.invoke` node.
A hook declares its input/output contract, phase, ordering, maximum invocations,
reentrancy and failure behavior. Hook output is normal typed state; there is no
magic `hook.*` namespace. Lifecycle is a validated finite-state resource changed
only by explicit transition stages.

## Queues, locks and placement

Queues durably record admission ID, principal, priority, FIFO sequence,
idempotency key, lease, acknowledgement and cancellation/interrupt class.
`queue.drain` is an explicit stage and returns ordered typed input. Requeue,
overflow and stale-lease behavior are declared.

Locks declare a backend/coordinator, resource-key expression, owner/run ID,
lease and renewal bounds, timeout behavior and a monotonic fencing token. A
mutation stage must present the current token. Checkpoint and terminal release
are explicit; a stale worker cannot resume a protected mutation.

Placements are a closed tagged union of local, worktree and remote execution.
Each declares workspace creation/cleanup, mount/network/effect constraints,
host selection and supported platforms. Selected placement and affinity are
persisted before execution; create/exec/poll/cancel for a job remain host-affine.
Unsupported effects or platforms fail validation rather than degrading.

## Events, checkpoints and resumability

Every model-visible input and output is reconstructable from the append-only,
schema-versioned event stream without rereading mutable workspace files or
calling a provider. Events carry config digest, run/session/branch identifiers,
monotonic sequence, event ID, causation/correlation IDs, payload type/version,
payload digest and lifecycle transition. Model-visible payload bytes are stored
inline or in the session's content-addressed store. A digest without retained
bytes is not replay.

Events cover source snapshots, assembled prompt parts and cache boundaries,
normalized provider requests/messages, Bashy preflight/results and partial job
output, policy decisions, queue steering, compaction, subagent handoffs,
interrupts, lifecycle transitions and delivery. Telemetry content recording is
independent: `observability.recordContent: false` prevents content export but
does not remove replay payloads from trusted local state.

`checkpoint.save` is an explicit graph stage and names the exact event sequence,
typed state and placement/lock/queue leases it covers. HITL `ask` saves a
checkpoint before waiting. Resume accepts `approve`, `edit` or `reject` with an
authenticated actor and one-use decision ID. Approval uses the execution
binding; edit produces a new intent that repeats preflight and policy evaluation.
Duplicate, stale or differently bound decisions fail closed.

## Frontends, triggers and sinks

Canonical input and canonical events are invariants of every frontend and are
therefore not configurable flags. Local stdio/embed frontends declare their
local trust boundary. Every network frontend declares endpoint, authentication,
TLS and request limits; WebSocket or NATS never inherit HTTP authorization
implicitly.

Each enabled trigger explicitly routes authenticated input to an `agentRef` and
`queueRef`, with input mapping, idempotency and admission limits. Scheduled
triggers also declare timezone, misfire and overlap policy. Each delivery stage
names `sinkRefs`. A sink declares credentials, destination allowlist, payload
redaction, bounded retry/backoff, deduplication and dead-letter behavior. There
is no unrouted ingress or implicit primary output.

## Extension and import rule

Unknown fields fail. Experimental data may appear only under
`metadata.annotations` or `spec.extensions`, keyed by a reverse-DNS owner. The
kernel never changes behavior because an extension exists unless the document
also references a registered, neutral, versioned extension stage. Compatibility
profiles must not introduce product names into the stage registry.

Skills and imports are immutable inputs. They declare a digest, exported source
or pipeline references, input/output schemas and effect ceiling. Catalog content
cannot inject executable Go, model tools, ambient defaults or unregistered
stages. Compilation records all resolved digests in the closed artifact.

## Compiler gates

The compiler must reject:

1. duplicate or unknown keys, YAML aliases, custom tags and multiple documents;
2. undeclared environment reads, scalar secrets or secret-bearing diagnostics;
3. escaping/symlinked content paths, overlapping control/model roots and
   unbounded or mutable replay content;
4. missing, type-confused, unreachable or cyclic references;
5. hidden predicates, untyped state, reads-before-production, invalid branch
   joins or escaping loop variables;
6. unbounded loops/retries/fallback/fan-out or unmatched failure outcomes;
7. any model-visible tool other than the generated `bashy` contract;
8. wildcard grants, incomplete preflight or authority/budget escalation;
9. frontends, triggers or sinks that bypass canonical input/events, routing,
   authentication or delivery bounds;
10. mutable/unpinned imports, skills, commands or executables;
11. a runnable harness lacking an interactive-unavailable HITL decision;
12. unsupported platform/effect combinations or unfenced protected mutations.

`examples/agent.yaml` is the canonical executable self-hosting fixture. The
Codex-like, OpenCode-like, OpenClaw-like and Hermes-like documents must use the
same schema, stage catalog, normalized message/event contracts and Bashy result
family.

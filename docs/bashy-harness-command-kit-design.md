# Bashy harness command and utility kit

Status: Sprint 106 design for story `3f0923eb762b`. This document synthesizes
the [Codex](codex-harness-primitives-audit.md),
[OpenCode](opencode-harness-primitives-audit.md),
[OpenClaw](openclaw-harness-primitives-audit.md), and
[Hermes](hermes-harness-primitives-audit.md) extraction audits against the
current Bashy/coreutils surface. It specifies a neutral mechanism kit, not a
compatibility mode for any of those products.

## Decision

Add one Bashy harness-runner mechanism with seven orthogonal operations:
`preflight`, `execute`, `start`, `poll`, `stdin`, `signal`, and `cancel`. It is
the implementation behind the single model-visible tool named `bashy`; the
operations are not seven tools and do not become seven shell verbs.

Do not add Bashy commands for context assembly, session state, events,
compaction, queues, approvals, placement, hooks, locks, subagents, triggers, or
sinks. Those facilities require trusted durable state or encode graph policy.
They therefore belong to neutral Go mechanisms selected and ordered by YAML.
In particular, Bashy must never receive access to `runtime.controlRoot`.

The resulting boundary is:

- **B — Bashy/coreutils:** resolve and classify commands, describe an exact
  effect intent, execute only an authorization-bound intent, supervise the OS
  process, bound binary output, and apply process control.
- **Y — YAML:** choose stages, routes, limits, authority, effect policy,
  approval, concurrency, placement, retries, safe points, hooks, triggers, and
  sinks.
- **G — neutral Go:** validate types and bindings; persist events, state, CAS,
  leases, checkpoints, queues, outboxes, and lifecycle receipts; normalize
  providers; and interpret the compiled graph without product branches.

This is the command-side refinement of the
[canonical harness boundary](harness-schema-design.md#the-bashy-boundary).

## Minimal operation set

### Bashy runner operations

| Operation | Required behavior | Governed effect | Why it is irreducible |
|---|---|---|---|
| `preflight` | Parse the exact script or argv; resolve cwd, executables, canonical targets, destination classes, credential references, limits, and placement facts; join every command to its Command Atlas record; return a canonical intent and completeness verdict. | No requested workload effect. Its own observation reads are audit facts. Unknown or dynamic effect closure returns `complete: false`. | Static analysis must precede YAML policy and produce the digest that later authorizes execution. |
| `execute` | Run one foreground intent only when an opaque authorization binding matches the complete preflight, config/state revisions, grant, limits, and placement. Return one terminal result. | Exactly the effects in the bound intent; never a wider inferred set. | Foreground completion has different durability and output behavior from a supervised job. |
| `start` | Persist an execution reservation before spawn, then start one supervised job with an opaque logical ID, placement affinity, generation, and output cursors. Reusing the idempotency key returns the same reservation/result. | Exactly the effects in the bound intent. | A kernel PID and shell job table are not durable or portable identities. |
| `poll` | Return monotonic lifecycle state and binary-safe output chunks after caller-provided cursors; never rerun the command. | Observation only. | Restart reconciliation and bounded streaming require cursor-based reads. |
| `stdin` | Append a sequenced, bounded input chunk to a running job when the preflight and policy explicitly allowed interactive input. Duplicate sequence/digest is idempotent. | `write` to the process input channel. | Input is state-changing and cannot be disguised as polling or host file access. |
| `signal` | Apply one portable semantic signal, or return typed `unsupported`; bind the request to job generation and idempotency key. | `write` to process control. | Explicit interrupt/terminate semantics are distinct from lifecycle cancellation. |
| `cancel` | Record durable cancellation intent, stop the process tree, close input, settle output, and return `cancelled`, `completed`, or `ambiguous`. Repetition is idempotent. | `write` to process control; the original workload may already have produced its bound effects. | Graph cancellation needs a terminal/reconcilable contract rather than best-effort signaling. |

There is deliberately no `approve` operation: YAML `policy.evaluate` and a G
approval checkpoint create the opaque authorization binding. There is no
`resume` operation: G reconciles a job through `poll` using its durable handle.
There are no `tail`, `read-file`, or `write-file` operations: bounded Bashy and
coreutils commands already cover those OS operations.

### YAML-owned stages and decisions

YAML declares the graph nodes that call the runner and the mechanisms below.
The names are schema stage kinds, not Bashy commands:

```text
context.snapshot -> context.measure -> [compact -> checkpoint]
 -> model.sample -> call.normalize -> schedule.calls
 -> forEach(call) {
      bashy.preflight -> policy.evaluate
      -> [approval.wait -> approval.resume]
      -> bashy.execute | bashy.start -> bashy.poll
    }
 -> queue.drain -> loop | complete
```

YAML also owns duplicate-intent policy, ordered/sequential barriers, maximum
parallelism, time and output budgets, retry and ambiguity routes, queue safe
points, hook order/failure behavior, placement choice, lock scope, compaction
threshold and successor route, delegation roster/caps/join, trigger admission,
and sink delivery/reconciliation. A Go switch on `codex`, `opencode`,
`openclaw`, `hermes`, provider name, tool name, or frontend is forbidden.

### Neutral Go mechanisms

The minimum G kit is deliberately storage- and transition-oriented:

- `state.read`, `state.cas`, `event.append`, `event.replay`,
  `checkpoint.write`, `replay.verify`;
- `queue.enqueue`, `queue.claim`, `queue.ack`, `queue.release`;
- `lease.acquire`, `lease.renew`, `lease.release`, plus lifecycle generation
  fencing;
- `approval.wait`, `approval.resolve`, `policy.checkBinding`;
- `context.snapshot`, `context.measure`, `compact.publish`,
  `successor.accept`;
- `schedule.plan`, `hook.dispatch`, `agent.invoke`, `agent.join`,
  `grant.intersect`, `placement.resolve`;
- `outbox.reserve`, `outbox.settle`, and adapter reconciliation;
- provider message normalization, deterministic call-ID repair, structured
  output validation, and typed expression evaluation.

Each invocation is reached through a compiled YAML node and emits the common
harness event envelope. These mechanisms may enforce invariants; they may not
choose workflow order, defaults, policy, routes, or product behavior.

## Wire contract

The request and result are JSON objects, UTF-8 encoded, with unknown fields
rejected for the advertised major version. Maps are canonicalized by sorted
keys; arrays preserve semantic order. Digests use lowercase
`sha256:<hex>`. Timestamps are RFC 3339 UTC with nanoseconds. Durations are
integer milliseconds. IDs are opaque strings and are never paths, PIDs, or
secrets.

### Request envelope

```json
{
  "schemaVersion": "bashy.harness.request/v1alpha1",
  "requestId": "req_opaque",
  "operation": "preflight",
  "binding": {
    "runId": "run_opaque",
    "nodeId": "tool.execute",
    "attempt": 1,
    "stateRevision": 42,
    "configDigest": "sha256:...",
    "lifecycleGeneration": 3,
    "idempotencyKey": "opaque"
  },
  "command": {
    "script": "printf '%s\\n' hello",
    "argv": [],
    "cwd": "/workspace",
    "environment": [
      {"name": "TOKEN", "valueRef": "secret://provider/token"}
    ]
  },
  "limits": {
    "wallTimeMs": 30000,
    "stdoutBytes": 1048576,
    "stderrBytes": 1048576,
    "stdinBytes": 0
  },
  "placement": {"id": "placement_opaque", "generation": 7}
}
```

Exactly one of `command.script` and non-empty `command.argv` is allowed.
`execute` and `start` additionally require `authorizationBinding` and
`intentDigest`. `poll` requires `jobId` plus stdout/stderr cursors. `stdin`
requires `jobId`, generation, sequence, encoding, bytes, and content digest.
`signal` requires a semantic signal. `cancel` requires `jobId` and generation.
Job operations retain the original run/node/attempt binding.

The model may name an admitted environment variable but never receives its
value. Durable records contain the reference and a keyed value digest after
trusted resolution, not secret bytes. A changed value invalidates a prior
binding without disclosing the value.

### Result envelope

```json
{
  "schemaVersion": "bashy.harness.result/v1alpha1",
  "requestId": "req_opaque",
  "operation": "execute",
  "protocol": "ok",
  "outcome": "completed",
  "binding": {
    "runId": "run_opaque",
    "nodeId": "tool.execute",
    "attempt": 1,
    "stateRevision": 42,
    "configDigest": "sha256:...",
    "lifecycleGeneration": 3,
    "idempotencyKey": "opaque"
  },
  "intent": {
    "digest": "sha256:...",
    "complete": true,
    "atlasDigest": "sha256:..."
  },
  "effects": [
    {
      "kind": "write",
      "scope": "workspace",
      "target": "/workspace/out.txt",
      "source": "redirection",
      "certainty": "exact"
    }
  ],
  "process": {
    "jobId": "job_opaque",
    "generation": 1,
    "exitCode": 0,
    "signal": null,
    "startedAt": "2026-09-06T20:00:00.000000000Z",
    "finishedAt": "2026-09-06T20:00:00.010000000Z",
    "durationMs": 10
  },
  "output": {
    "stdout": [{"from": 0, "to": 6, "encoding": "base64", "data": "aGVsbG8K"}],
    "stderr": [],
    "stdoutNext": 6,
    "stderrNext": 0,
    "truncated": false,
    "artifactRefs": []
  },
  "portability": {"platform": "darwin/arm64", "unsupported": []},
  "error": null
}
```

`protocol` is `ok` when Bashy understood and settled the operation, even when
the command exits nonzero. `outcome` is one of `prepared`, `running`,
`completed`, `blocked`, `cancelled`, `timedOut`, `failed`, or `ambiguous`.
`exitCode`, signal, timeout, and output-limit observations do not become
transport errors. Output chunks are ordered by byte cursor and use `base64` so
arbitrary process bytes survive JSON; an artifact is content-addressed and its
digest/size/media type are part of the receipt.

The result includes operation-specific data without changing the common
fields: preflight adds resolved commands/destinations/credentials and an
unsupported list; start adds the durable job receipt; poll adds lifecycle and
chunks; stdin adds accepted sequence; signal/cancel add a control receipt.

## Effects and authorization

The Command Atlas has a closed eleven-atom vocabulary:
`pure`, `read`, `write`, `destroy`, `net`, `exec`, `cred`, `priv`, `remote`,
`persist`, and `spend` ([definitions](../../coreutils/pkg/atlas/atlas.go#L113)).
An Atlas entry is a conservative command-level maximum. Preflight refines it
into per-intent facts with target, scope, source, and certainty; refinement may
retain or narrow a maximum, never remove an effect without a command-specific
proof. The effective effect set is the union across shell operations,
redirections, expansions, invoked commands, placement, and credentials.

`complete: true` means every executable, path/destination class, credential,
and governed effect is resolved under the exact runtime inputs. Dynamic
command construction, an unclassified executable, an unavailable Atlas record,
an uncanonicalizable target, or analysis beyond a declared limit produces
`complete: false`. YAML policy must deny incomplete intent. It may not turn an
unknown into `pure`.

G persists preflight before policy evaluation. An authorization binding covers
the full canonical intent, effect facts, Atlas snapshot digest, script/argv,
cwd, environment names and value digests, limits, placement/generation,
effective grant, run/node/attempt, state revision, config digest, policy
revision, expiry, and one-use/idempotency scope. Bashy compares that binding
immediately before spawn. Hook mutation or any drift requires a fresh
preflight and policy decision.

The existing Bashy command audit remains supplementary execution evidence. It
is explicitly an evidence record rather than containment or a complete process
tree boundary ([audit contract](../../coreutils/pkg/policy/audit/audit.go#L5));
it cannot replace the canonical G event store or the preflight/policy binding.

## Failure and replay semantics

Protocol failures use a closed `error.code` vocabulary:
`invalidRequest`, `unsupported`, `unavailable`, `incompletePreflight`,
`authorizationRequired`, `bindingMismatch`, `staleGeneration`, `jobNotFound`,
`outputLimit`, `internal`, and `ambiguousEffect`. An error includes a safe
message, retryability, and structured details; it never includes secret values
or raw host diagnostics outside the declared output policy.

- Invalid, unknown, unavailable, stale, or incomplete authorization fails
  closed before spawn.
- Command exit 1, a signal, timeout, and policy-approved output truncation are
  typed workload outcomes, not protocol failure.
- The intent/reservation event is durable before an effect. Settlement is
  durable after it. Retrying one idempotency key returns the existing receipt.
- If the runner cannot prove whether an effect happened, it returns
  `ambiguous`; G records that fact and follows a YAML reconciliation route. It
  never blindly reruns.
- Every admitted call ID settles once. Results retain model order even when
  YAML permits bounded concurrent execution.
- Replay reads recorded receipts and performs zero Bashy, model, hook, child,
  or sink I/O.

## Portability contract

The envelope is identical on Darwin, Linux, and Windows. Platform differences
are capability facts and typed `unsupported` results, not alternate schemas or
hidden YAML defaults.

- Job identity is an opaque runner ID plus generation. PID may be volatile
  diagnostic data but is never authorization, affinity, or replay identity.
- `interrupt`, `terminate`, and `kill` are portable semantic signals. Other
  signal names require a declared platform capability and may return
  `unsupported`. Cancellation targets the supervised process tree/job object.
- Paths retain platform and volume identity, absolute canonical form, symlink
  resolution evidence, and case-sensitivity facts. Cross-platform path strings
  are not assumed equivalent.
- Output is byte-cursored and binary-safe. Cursor advancement is monotonic;
  bounded in-envelope chunks may spill to content-addressed artifacts.
- A durable job is placement-affine. If its supervisor cannot be reached after
  restart, `poll` returns unavailable/ambiguous evidence and G follows the
  YAML recovery route; another host must not relaunch it.
- The runner's private supervisor state and output spool are not command-visible
  paths. `runtime.controlRoot` remains exclusively G-owned.

The existing coreutils jobs facility cannot implement this contract: its
record key is the kernel PID ([registry](../../coreutils/pkg/jobs/registry.go#L21)),
it prunes records using local PID liveness
([list](../../coreutils/pkg/jobs/registry.go#L76)), and its signal-based
operations are unsupported on Windows
([Windows adapter](../../coreutils/pkg/jobs/jobs_windows.go#L26)). It may remain
an interactive shell convenience but is not a harness backend.

## Existing commands reused

The live inventory command is `bashy commands --atlas --json`, which currently
emits `bashy-atlas-v1`. The catalog is curated rather than inferred and has
closed groups, tiers, capabilities, stages, effects, and a coverage ratchet
([Atlas contract](../../coreutils/pkg/atlas/atlas.go#L5)). The harness reuses the
following surface instead of inventing synonyms:

| Existing surface | Harness use | Limit; do not reinterpret as |
|---|---|---|
| `commands --atlas --json` | Resolver inventory and conservative command classification. | A per-intent effect proof or policy decision. |
| `check --agent --script` (`bashy-check-v1`) | Syntax, recursive script-reference, and command-resolution diagnostics ([implementation](../../bashy/internal/agentos/check.go#L21)). | Complete effect closure or authorization. |
| `--dry-run` agent manifest | Non-executing preview of resolved external commands and selected file destruction/redirection ([handlers](../../bashy/internal/agentos/dryrun.go#L120)). | A complete proof: current analysis is intentionally partial and reads the live filesystem. |
| `run --check --capture` (`bashy-run-v1`) | Reuse its foreground process, exit/signal, duration, advisor, and capture primitives ([implementation](../../bashy/internal/agentos/run.go#L30)). | The harness wire contract: it lacks authorization binding, durable jobs, binary cursors, and complete effects. |
| `timeout`, `tokens --json` | Bounded child execution where selected by a Bashy script; deterministic token/byte measurement. | Graph deadline/retry policy or session budget state. |
| `sha256sum`, `b2sum`, `realpath`, `readlink`, `stat`, `find` | Digests and root-bounded filesystem facts used by preflight/context stages. | Trusted state/CAS, admission, or symlink policy. |
| `head`, `tail`, `wc`, `sort`, `uniq`, `jq`, `base64` | Deterministic bounded fragments, canonical reductions, and byte transport. | Compaction, prompt ordering, or memory policy. |
| `context --json` | First-hop Bashy/runtime capability facts. | A durable prompt/context snapshot. |
| `activity`, `bus`, `inbox`, `notify` | Optional commands inside explicitly authorized external adapter stages. | The canonical harness event log, durable input queue, trigger, or outbox. |
| `schedule --json` | Operator-facing local schedule management when a graph explicitly calls it. | Harness trigger timing or restart policy; those remain Y/G. |
| `audit` | Tamper-evident Bashy command evidence and export/verification. | Canonical replay events or enforcement. |
| `claim` | Existing project path-set collaboration claims. | Harness queue claims, leases, placement, or locks. |
| `delegate`, `chat` | Ordinary external agent CLI commands when explicitly invoked and authorized through Bashy. | Neutral `agent.invoke`, parent/child durability, or delegation policy. |
| `podman`, `sandbox`, `weave` | Governed execution targets/commands after YAML placement resolution. | Placement selection, grants, or retry policy. |
| `dhnt` | Existing portable pipeline/run validation and evidence transforms. | Harness state/event storage. Its live Atlas record currently omits `effects`; consumers must treat that as unclassified and fail closed until catalog coverage is corrected. |
| shell `jobs`, `wait`, `kill` and coreutils jobs | Interactive, process-local shell control only. | Durable harness job operations. |

## Genuinely new utilities

Only these command-side mechanisms are new, and they ship as one harness-runner
implementation rather than independent product tools:

1. **Complete intent compiler.** Combines Bashy parsing/resolution, Atlas
   maxima, shell redirections/expansions, canonical paths/destinations,
   credential references, placement, and limits into a closed preflight report
   and digest. Existing `check` and dry-run feed it but do not define it.
2. **Authorization-bound executor.** Verifies the opaque binding immediately
   before spawn and produces the stable result envelope. Existing `run`
   process/capture primitives are reused beneath it.
3. **Portable durable supervisor.** Implements logical job IDs, generations,
   idempotent reservation, process-tree cancellation, semantic signals,
   restart reconciliation, and placement affinity without PID identity.
4. **Binary bounded spool.** Stores stdout/stderr by monotonic byte cursor,
   returns bounded base64 chunks, and spills content-addressed artifacts without
   exposing private paths.

No public `context`, `session`, `event`, `memory`, `compact`, `queue`, `approve`,
`place`, `hook`, `lock`, `agent`, `trigger`, or `sink` command is added for the
harness. Existing same-named verbs retain their current meanings.

## Command Atlas integration

The runner itself is the `spec.bashy` tool transport, not a command that a
model-authored Bashy script can recursively invoke, so it requires no new
model-addressable Atlas entry. Every executable inside an intent must resolve
to an Atlas entry. The preflight result records:

- Atlas schema version and digest;
- name, resolver, class, tier, stage, output shape, caps, maximum effects, and
  executable/content digest for each resolved command;
- dynamic refined effects and their proof source;
- every unclassified, missing, or unsupported item; and
- `complete`, which is false if any required field is unknown.

Atlas records describe maximum capability; they do not encode YAML admission
policy. Operation-specific effects belong in the signed preflight report, not
as new Atlas command names. If a future operator-only diagnostic CLI exposes
the runner, it should be one front-door verb backed by the same implementation,
excluded from model-script resolution, and classified with the union of its
possible effects; that CLI is not required by this design.

## Acceptance criteria

1. The model receives exactly one tool named `bashy`; the advertised operation
   schema and result schema have stable digests.
2. Incomplete/unclassified preflight cannot produce an executable binding, and
   any command/cwd/env/limit/placement/grant/config/state drift rejects a prior
   binding.
3. Crash tests at reservation, spawn, output append, settlement, signal,
   cancellation, and replay converge to one terminal or explicitly ambiguous
   receipt without automatic duplicate effect.
4. Foreground and durable jobs preserve nonzero exit, signal, timeout,
   cancellation, output-limit, and arbitrary output bytes as typed facts.
5. Darwin, Linux, and Windows return the same envelope; unsupported process
   control is typed, and no durable identity depends on a PID or path.
6. Queue, policy, approval, schedule, hook, lock, compaction, placement,
   delegation, trigger, and sink choices can be changed in YAML without a Go or
   Bashy product branch.
7. Replay of a completed run performs no external I/O and reproduces the same
   state, event, checkpoint, intent, and output-artifact digests.

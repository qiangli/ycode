# Codex harness primitives: extraction audit

Status: Sprint 106 story `1ba0a93865e5`  
Evidence baseline: Codex `7e45bdb5fd0e6cae3aeb14330deed7861bb516da`  
Scope: read-only audit of `priorart/codex`; this document is design evidence, not an import plan.

## Decision

Codex contains useful correctness mechanisms, but its runtime is not a reusable
harness contract. Bashy's harness should preserve the observable mechanisms
below while moving every sequencing, routing, limit, retry, approval, and
retention choice into the YAML graph. Go may implement typed, product-neutral
mechanisms. Bashy/coreutils may implement deterministic command utilities. No
stage may branch on `codex`, `opencode`, `openclaw`, `hermes`, a provider name,
or a product-specific protocol variant.

The ownership labels used in the map are:

- **B** — reusable Bashy/coreutils utility: OS-facing facts and effects, with a
  stable request/result envelope and no graph policy.
- **Y** — YAML graph policy: visible order, routing, budgets, limits, failure
  behavior, grants, retention, and user-defined composition.
- **G** — neutral Go mechanism: typed state transition, durable storage,
  concurrency safety, or protocol-neutral provider transport.

## Evidence-linked extraction map

| Harness behavior | Codex evidence | Extract | Owner and required contract |
|---|---|---|---|
| Context fragments | [`RenderedFragment` carries role and annotated content; `ContextualUserFragment` supplies kind, markers, body, and message separation](../priorart/codex/codex-rs/context-fragments/src/fragment.rs#L8) | Typed fragment and provenance envelope; deterministic rendering | **G** renders typed fragments. **Y** selects sources, order, markers, trust, per-source and total budgets. |
| Repository instructions | [`load_project_instructions` walks bounded environments and records source path, environment, and cwd](../priorart/codex/codex-rs/core/src/agents_md.rs#L55) | Root-bounded deterministic discovery, byte accounting, provenance | Filesystem walk can be a **B** utility; typed snapshot is **G**. **Y** owns filenames, root, symlink policy, trust gate, precedence, and overflow outcome. Never copy Codex's implicit defaults. |
| Context accounting | [`ContextWindowTokenStatus` separates active and compaction scopes](../priorart/codex/codex-rs/core/src/session/context_window.rs#L8) | Saturating token accounting with explicit scope | **G** reports measurements only. **Y** owns thresholds, reserved headroom, fallback behavior, and compaction route. |
| Model-visible step snapshot | [`run_turn` captures the first step context before sampling and reuses a turn-scoped client session](../priorart/codex/codex-rs/core/src/session/turn.rs#L155) | Immutable input snapshot, config digest, and attempt identity | **G** creates the snapshot and event. **Y** defines when to refresh it and which fragments enter it. |
| Agent loop | [The turn loop executes calls, records their outputs, and samples again until an assistant-only response](../priorart/codex/codex-rs/core/src/session/turn.rs#L145) | Typed `continue`, `complete`, `retry`, `blocked`, `cancelled`, and `failed` outcomes | **Y** expresses the loop and routes every outcome. **G** validates transitions and executes stages; it must not hide a loop in Go. |
| Compaction | [Pre-sampling compaction is an explicit operation](../priorart/codex/codex-rs/core/src/session/turn.rs#L1053) and [mid-turn handling is embedded in the loop](../priorart/codex/codex-rs/core/src/session/turn.rs#L420) | Token measurement, summary production, history replacement, checkpoint | **Y** owns trigger expressions, prompt/route, preservation set, retry, and placement before or within the loop. **G** applies a typed CAS state transition. |
| Steering/input mailbox | [`TurnInput` and the mailbox preserve typed input and turn provenance](../priorart/codex/codex-rs/core/src/session/input_queue.rs#L22); [drain/deferral occurs at controlled delivery phases](../priorart/codex/codex-rs/core/src/session/input_queue.rs#L289) | Typed durable FIFO, stable message ID, parent/root turn IDs, enqueue sequence, delivery phase, lease/ack | Queue storage and atomic drain are **G**. **Y** declares safe drain points, supersession, batching, deferral, and route. An in-memory mailbox is insufficient for replay. |
| Provider retry | [Sampling preserves input across attempts, refreshes history, and distinguishes retryable failures](../priorart/codex/codex-rs/core/src/session/turn.rs#L1391) | Normalized attempt/result/error envelope and reconnectable stream session | Transport is **G**. **Y** owns retry classes, counts, backoff, jitter, exhaustion route, and whether state is refreshed. |
| Parallel tool calls | [Parallel-safe calls take a shared read lock while serial calls take the write lock](../priorart/codex/codex-rs/core/src/tools/parallel.rs#L116) | Bounded scheduler, cancellation token, terminal-outcome guard, deterministic collection index | Scheduler is **G**. **Y** owns `maxParallel`, ordering, timeout, failure aggregation, and which graph nodes may overlap. |
| Command execution and jobs | [Unified exec defines process lifecycle and a canonical noninteractive environment](../priorart/codex/codex-rs/core/src/unified_exec/process_manager.rs#L1); [head/tail buffering makes truncation explicit](../priorart/codex/codex-rs/core/src/unified_exec/head_tail_buffer.rs#L1) | `preflight`, `execute`, `start`, `poll`, `stdin`, `signal`, `cancel`; bounded output with omitted-byte count or spill reference | **B**. This is the sole model-facing tool family. The envelope must return intent digest, effects, exit/signal, timings, stdout/stderr truncation metadata, and artifact references. **Y** decides when it may run. |
| Command risk facts | [Exec policy identifies unsafe broad command-prefix approvals](../priorart/codex/codex-rs/core/src/exec_policy.rs#L1) | Canonical argv/cwd/env/resource intent and declarative effect facts | Canonicalization and Command Atlas lookup are **B** facts. **Y** converts facts to allow/ask/deny; no hard-coded Go policy or provider-specific exception. |
| Approval | [`ApprovalAction` is typed by effect and binds execution context](../priorart/codex/codex-rs/core/src/tools/approvals.rs#L67) | Durable one-use checkpoint binding exact preflight result, config digest, state version, scope, expiry, and approver decision | Binding verification and CAS resume are **G**. Evaluation, authority, expiry, escalation, and routes are **Y** via an explicit `policy.evaluate` stage. Approval UI is a replaceable frontend/sink. |
| Hooks | [Dispatcher selects event/matcher handlers, supports sync/async execution, and restores configured result order](../priorart/codex/codex-rs/hooks/src/engine/dispatcher.rs#L1) | Typed event matching, ordered dispatch, async join, block/continue outcome | Dispatch/join are **G**; a hook script executes through **B**. **Y** declares event, matcher, stage pipeline, sync mode, mutation authority, timeout, and failure route. |
| Subagents | [Fork filtering explicitly chooses which rollout items survive](../priorart/codex/codex-rs/core/src/agent/control/spawn.rs#L63); [effective permissions are intersected with authority](../priorart/codex/codex-rs/core/src/agent/control/spawn.rs#L520) | Durable parent/child edge, child state branch, monotonic grant intersection, bounded reservation, join/cancel | Graph store, `agent.invoke`, branch/CAS, and grant intersection are **G**. **Y** owns roster resolution, context projection, budgets, depth, concurrency, placement, join, cancellation, and retention. |
| Durable event log and replay | [Recorder uses a bounded writer channel](../priorart/codex/codex-rs/rollout/src/recorder.rs#L933), [append mode and pending suffixes protect ordering across write failures](../priorart/codex/codex-rs/rollout/src/recorder.rs#L1654) | Append-only event envelope with run/event/causation/correlation IDs, monotonic sequence, schema version, config digest, state before/after versions, attempt, outcome, and redaction metadata | **G** owns atomic append, idempotency, indexes, CAS, checkpoint, and replay validation. **Y** owns retention, compaction, redaction route, and sinks. |
| Event completeness | [Rollout policy deliberately filters live events](../priorart/codex/codex-rs/rollout/src/policy.rs#L28) | Treat Codex rollout as resumable conversation history, not exact replay evidence | Bashy must persist every model-visible input/output, queue decision, policy decision, approval, retry, hook result, side-effect intent/result, and state transition. Telemetry may be separately sampled; replay-critical events may not be filtered in Go. |
| Memory extraction | [Phase 1 requires strict structured output and bounded parallel work](../priorart/codex/codex-rs/memories/write/src/phase1.rs#L54) | Typed extraction result, claim/lease, secret redaction, bounded worker utility | Claim/lease/store/redaction are **G**; deterministic file/diff helpers may be **B**. Selection, prompt, model route, concurrency, retry, and failure are **Y**. |
| Memory consolidation | [Phase 2 is a numbered workflow with a global lock](../priorart/codex/codex-rs/memories/write/src/phase2.rs#L1) | Lock/heartbeat/ownership token, watermark, deterministic input set, atomic publish | Lock and publish are **G**. The entire consolidation workflow is **Y**, including triggers, placement, model choice, bounds, and sinks; do not preserve it as hard-coded Go orchestration. |
| Engineering workflow | [The turn loop treats tool results as observations for the next sample](../priorart/codex/codex-rs/core/src/session/turn.rs#L145) | Shell-driven inspect, search, patch, format, build, test, and version-control steps | Commands are **B** calls. Planning, verification gates, retry, approval, and completion criteria are **Y** nodes. There is no separate product-specific edit/test/git tool registry. |

## Required interfaces

### Bashy/coreutils boundary

The model sees one tool, `bashy`. Its operations share one versioned envelope:

```yaml
request:
  operation: preflight | execute | start | poll | stdin | signal | cancel
  command: {argv: [], cwd: "", env: {}}
  limits: {wallTime: "", outputBytes: 0}
  binding: {runId: "", nodeId: "", attempt: 0, stateVersion: 0}
result:
  intentDigest: "sha256:..."
  effects: []
  outcome: success | failure | timeout | cancelled | running
  process: {id: "", exitCode: 0, signal: ""}
  output: {stdout: "", stderr: "", omittedBytes: 0, artifact: ""}
```

`preflight` is effect-free and `execute` must prove it is executing the same
canonical intent. Approval binds the preflight digest and current state/config;
a changed command, cwd, environment, limit, grant, or state version requires a
new evaluation. Job IDs and artifact references are durable opaque values, not
host PIDs or temporary paths.

### Neutral Go stage mechanisms

The kernel may expose these typed mechanisms, provided none contain workflow
order or product detection:

- `state.read`, `state.cas`, `event.append`, `checkpoint.write`, `replay.verify`
- `queue.enqueue`, `queue.claim`, `queue.ack`, `queue.release`
- `lock.acquire`, `lock.renew`, `lock.release`
- `context.snapshot`, `context.measure`, `model.sample`
- `policy.check_binding`, `approval.resume`
- `hook.dispatch`, `agent.invoke`, `agent.join`, `grant.intersect`
- `memory.query`, `memory.claim`, `memory.publish`

These are executor capabilities, not implicit stages. A run may invoke them only
through compiled YAML nodes, and each invocation emits the common durable event
envelope.

### YAML graph ownership

A Codex-like profile is consequently a declarative graph, not a Go mode:

```text
trigger -> context.snapshot -> queue.drain -> context.measure
        -> [compact -> checkpoint] when expression matches
        -> model.sample -> classify typed outcome
        -> forEach(tool call, bounded) {
             bashy.preflight -> policy.evaluate
             -> [approval checkpoint -> resume] when asked
             -> bashy.execute -> event.append
           }
        -> queue.drain -> loop | complete
        -> hooks/sinks routed from declared events
```

Loop-carried state must include the durable message/event cursor, context
snapshot ID, queue watermark, model-session reference, attempt counters, budget
usage, pending calls, approval bindings, child edges, and checkpoint version.
Every expression is typed and every node declares all possible outcomes and
routes. Triggers and sinks are named graph edges; they are not callbacks
registered from Go.

## Explicit non-extractions

Do not copy any of the following Codex design choices into hidden runtime code:

- the built-in tool registry, tool-name dispatch, or separate patch/shell/MCP
  execution branches;
- model-specific context defaults, token thresholds, retry counts, or fallback
  prompts;
- implicit pre-turn/mid-turn compaction or hard-coded queue drain safe points;
- rollout event filtering that makes policy, hook, queue, or approval decisions
  unreplayable;
- plugin-specific hook execution paths;
- V1/V2, client, provider, or agent-product compatibility branches;
- subagent history projection, depth, concurrency, or child permission policy;
- memory startup eligibility, phase ordering, prompts, selection limits, or
  global workflow locks as a fixed Go routine.

Compatibility belongs at input/output adapters. After normalization, all
frontends compile to the same schema, typed state, Bashy tool envelope, event
contract, and executor mechanisms.

## Security and replay acceptance

The extracted design is acceptable only when golden traces demonstrate:

1. A fresh run and replay produce the same node order, expressions, state
   versions, policy outcomes, queue delivery, and model-visible transcript.
2. A crash after an effect cannot duplicate it: intent digest plus idempotency
   key resolves the prior result before retry.
3. A stale approval, lease, lock token, or state version fails closed.
4. Child grants are a monotonic intersection and can never exceed the parent or
   placement authority.
5. Context discovery cannot escape the declared trusted root through `..`,
   symlinks, alternate worktrees, or environment-controlled paths.
6. Secrets are redacted before durable model-visible storage, while audit
   metadata still proves which redaction policy/version ran.
7. Hook, trigger, sink, queue, compaction, memory, and subagent behavior can be
   changed by YAML without recompiling Go.
8. The same compiled graph runs for Codex, OpenCode, OpenClaw, and Hermes inputs
   without a product-name branch in the runtime.

## Extraction priority

1. **P0:** durable event/CAS/checkpoint contract, complete replay evidence,
   canonical Bashy preflight/execute binding, and explicit `policy.evaluate`.
2. **P0:** typed loop outcomes, durable queue safe points, compaction as graph
   stages, and crash-safe effect idempotency.
3. **P1:** bounded scheduler, hook dispatch, child-edge persistence, authority
   intersection, and declared join/cancel behavior.
4. **P1:** root-bounded context snapshots with provenance and typed token
   accounting.
5. **P2:** memory claim/lease/redaction/publish mechanisms expressed through a
   fully YAML-owned extraction and consolidation graph.
6. **P2:** reusable Bashy job control and head/tail/spill utilities after the
   synchronous command envelope and replay invariants are locked.

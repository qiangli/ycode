# OpenClaw harness primitives audit

Status: Sprint 106 story `fce9f612a984` evidence map  
Audited prior art: `priorart/openclaw` at `33962654f307109335421fb662fe5282a2048be4`  
Scope: read-only extraction for the YAML-native Bashy harness; this document is not an OpenClaw compatibility plan.

## Decision

OpenClaw has strong evidence for durable lifecycle receipts, atomic admission, transcript repair, compaction handoff, outbox delivery, and restart fencing. Reuse those contracts, not its product topology. The harness remains a neutral graph interpreter: YAML owns policy and ordering; Go owns deterministic mechanisms; the only model-facing tool is Bashy.

Ownership labels:

- **B — Bashy:** command preflight/execution, isolation, credential firewall, cancellation, and typed process results.
- **Y — YAML:** graph topology, policies, expressions, routes, retries, schedules, hooks, placements, and sink selection.
- **G — neutral Go:** parsing, validation, durable event/state/CAS/lease/outbox mechanics, expression evaluation, and adapter interfaces. A G item may execute a YAML decision; it may not invent one.

## Evidence-linked extraction map

| Primitive | OpenClaw evidence | Owner | Extracted contract | Boundary / required change |
|---|---|---:|---|---|
| Gateway connection admission | [`connect-admission.ts`](../priorart/openclaw/src/gateway/server/ws-connection/connect-admission.ts#L160) validates protocol, role, scopes, capability, and origin before accepting a connection. | Y + G | A trigger adapter presents authenticated facts; an explicit YAML admission node returns allow/deny plus a durable reasoned receipt. G defaults unknown or missing evidence to deny. | Client names, role branches, default scopes, and origin exceptions are OpenClaw policy and must not appear in Go. |
| Channel envelope normalization | [`envelope.ts`](../priorart/openclaw/src/channels/inbound-event/envelope.ts#L75) constructs an inbound envelope from route/session/store facts. | G | Normalize every ingress into a typed event with source, subject, route, payload reference, observed time, idempotency key, and authentication evidence. Preserve the raw immutable input by digest/reference. | OpenClaw session keys and display formatting are adapter details, not the canonical event model. |
| Channel admission graph | [`decision.ts`](../priorart/openclaw/src/channels/message-access/decision.ts#L1) makes route, sender, command, and mention gates explicit; [`admission-decision-receipt.ts`](../priorart/openclaw/src/channels/message-access/admission-decision-receipt.ts#L3) records action, reason, coverage, evidence, and remediation. | Y + G | YAML declares ordered admission checks and `policy.evaluate`; G durably records input/config/policy digests, outcome, missing evidence, and next node. `unknown` and `unsupported` never authorize. | The observed check order is evidence for an explicit graph, not a hard-coded Go pipeline. |
| Turn lifecycle | [`run-channel-turn.ts`](../priorart/openclaw/src/channels/turn/run-channel-turn.ts#L168) names ingest, classify, preflight, resolve, assemble, dispatch, and finalize phases; [`types.ts`](../priorart/openclaw/src/channels/turn/types.ts#L434) exposes ordered stages. | Y + G | Represent stages and transitions in YAML; append a start/settle event for each attempt and carry typed loop state between them. | Do not add an OpenClaw “channel turn” runtime mode or fixed phase list to Go. |
| Transcript validation and repair | [`session-transcript-repair.ts`](../priorart/openclaw/src/agents/session-transcript-repair.ts#L3) normalizes tool calls and synthesizes missing results; [`tool-result-pairing.ts`](../priorart/openclaw/packages/agent-core/src/harness/session/tool-result-pairing.ts#L1) isolates neutral pairing logic. | Y + G | An explicit YAML repair node derives a new replay view and emits a typed repair report: changed, dropped, moved, synthesized, source digest, and output digest. Original events remain immutable. | Provider-specific repair belongs in an adapter. Repair must never be a silent load-time mutation. |
| Signed transcript content | [`session-transcript-repair.ts`](../priorart/openclaw/src/agents/session-transcript-repair.ts#L275) keeps valid signed reasoning blocks byte-stable and drops an invalid containing message. | G | Treat signed/opaque provider blocks as indivisible values; adapters validate them and return a typed keep/drop/error result. | G must not interpret provider reasoning or rewrite authenticated bytes. |
| Compaction successor resolution | [`compaction-successor.ts`](../priorart/openclaw/src/agents/embedded-agent-runner/compaction-successor.ts#L36) resolves a successor only for a consistent active-store identity. | Y + G | YAML declares compaction and successor edges. G records predecessor event watermark, successor snapshot/checkpoint digest, state schema version, and exact session/placement binding. | Legacy marker files and compatibility heuristics are not extracted. |
| Compaction successor acceptance | [`compaction-successor.ts`](../priorart/openclaw/src/agents/embedded-agent-runner/compaction-successor.ts#L176) accepts under the predecessor's exact claim, captures commit before publication hooks, and keeps the commit authoritative if observation fails. | G | Accept with CAS against predecessor revision and placement lease; commit successor plus event atomically, then enqueue observer hooks. Publication failure cannot roll back a committed state transition. | YAML selects when/how to compact; Go supplies only transaction and fencing semantics. |
| Loop/tool admission | [`tool-loop-admission.ts`](../priorart/openclaw/src/agents/tool-loop-admission.ts#L132) checks a batch in assistant order, reserves projected state atomically, and records denial without partial admission. | Y + G | YAML owns limits and loop decisions; G evaluates the whole proposed call batch against one revision and atomically admits all or none, emitting typed per-call outcomes. | Tool names, thresholds, safety rules, and steering policy must not be compiled into Go. |
| Sole model tool | [`exec.ts`](../priorart/openclaw/src/process/exec.ts#L146) shows bounded command execution and [`exec-result.ts`](../priorart/openclaw/src/process/exec-result.ts#L1) normalizes exit, signal, timeout, kill, truncation, and output-limit outcomes. | B | The model can request only `bashy`. Bashy returns a typed intent from preflight and a typed execution result including stdout/stderr artifacts, exit/signal, limits, timing, and cancellation. | Do not copy OpenClaw's tool roster, host/sandbox target branches, or environment-derived approval defaults. G only invokes the Bashy interface. |
| Durable/background commands | [`command-queue.ts`](../priorart/openclaw/src/process/command-queue.ts#L121) models lanes and generations; [`task-executor.ts`](../priorart/openclaw/src/tasks/task-executor.ts#L102) records queued/running/completed/failed task transitions. | Y + G + B | YAML declares queue, priority, concurrency, timeout, retry, and placement. G persists job state, lease generation, heartbeats, progress, and terminal result; Bashy owns the child process and cancellation. | An in-memory queue is insufficient for replay. Runtime-kind enums and lane defaults are product policy. |
| Approval checkpoint | [`operator-approval-store.ts`](../priorart/openclaw/src/gateway/operator-approval-store.ts#L1) is persistent and first-answer-wins; its execution bindings are explicit in [`operator-approval-store.ts`](../priorart/openclaw/src/gateway/operator-approval-store.ts#L248). | Y + G | `policy.evaluate` may route to a durable approval wait. Bind approval to config revision, state revision, Bashy intent digest, execution/run identity, audience, expiry, and lifecycle generation; resume by CAS and consume once. | Approval cannot be inferred from a tool name or reused after hook mutation, replay, compaction, or restart without an exact binding match. |
| Hooks | [`internal-hooks.ts`](../priorart/openclaw/src/hooks/internal-hooks.ts#L272) exposes deterministic registration order; [`fire-and-forget.ts`](../priorart/openclaw/src/hooks/fire-and-forget.ts#L90) shows why timeout-only in-memory hooks are not durable. | Y + G | YAML declares hook order, phase, timeout, failure policy, and whether mutation is allowed. Replay-critical hooks are outbox jobs with idempotency keys and receipts; mutations create a new intent digest and rerun preflight/policy. | No global mutable registry, dropped queue, or “fire-and-forget” effect may influence canonical state. |
| Schedules and triggers | [`service-contract.ts`](../priorart/openclaw/src/cron/service-contract.ts#L23) carries trigger evaluation, schedule keys, logical source identity, and caller authority; [`run-receipt-store.ts`](../priorart/openclaw/src/cron/store/run-receipt-store.ts#L30) defines recoverable reserved/running/settling/terminal phases. | Y + G | YAML owns schedule, timezone, misfire, catch-up, overlap, dedupe, and admission policy. G atomically reserves a run with trigger identity/config revision and moves it through typed recoverable phases. | There is no privileged `cron` path. Schedules are trigger adapters feeding the same graph and admission contract. |
| Trigger leases and restart | [`run-receipt-store.ts`](../priorart/openclaw/src/cron/store/run-receipt-store.ts#L473) validates owner/start/config identity before activation. | G | Use opaque durable lease tokens, revision CAS, expiry, and generation fencing; every nonterminal receipt is live or recoverable and every abandonment becomes terminal or releases its claim. | PID/start-time identity is implementation evidence, not a portable harness identity. |
| Delivery sinks and reconciliation | [`durable-delivery.ts`](../priorart/openclaw/src/channels/turn/durable-delivery.ts#L27) preserves unsupported, handled, failed, and “sent before error” outcomes; [`durable-delivery.ts`](../priorart/openclaw/src/channels/turn/durable-delivery.ts#L151) resolves destination/capability and reconciles durable sends. | Y + G | YAML routes to named sinks with address/auth/capability/retry/reconciliation policy. G commits an outbox item before I/O, adapters return typed partial/ambiguous receipts, and retry uses one idempotency key. | No direct transport send may bypass the outbox. Sink choice and best-effort versus required delivery are YAML policy. |
| Steering and queued input | [`attempt-queue-message.ts`](../priorart/openclaw/src/agents/embedded-agent-runner/run/attempt-queue-message.ts#L147) acknowledges steering only after the matching input reaches the transcript and rejects it when the active session ends or hands off. | Y + G | Persist input queue IDs, target execution/generation, ordering, delivery lease, accepted event watermark, and terminal disposition. YAML declares safe points and interrupt/coalesce policy. | Process-local promises and display queues are not durable acknowledgement. |
| Subagent sessions | [`subagent-registry.types.ts`](../priorart/openclaw/src/agents/subagents/registry/subagent-registry.types.ts#L61) separates restart receipt phases, execution state, and lifecycle generation; delivery state is explicit in [`subagent-registry.types.ts`](../priorart/openclaw/src/agents/subagents/registry/subagent-registry.types.ts#L128). | Y + G | A neutral `agent.invoke` node creates a typed parent/child edge with input/output schema, budget, placement request, join policy, generation, and cancellation lineage. Completion and delivery are separate durable states. | Agent roster, model choice, depth, delegation, and routing remain YAML. Do not reproduce one product-sized registry record. |
| Subagent persistence and outbox | [`subagent-registry.store.sqlite.ts`](../priorart/openclaw/src/agents/subagents/registry/subagent-registry.store.sqlite.ts#L1) persists normalized identity/state; restart recovery verifies exact ownership before effects in [`subagent-registry-restart-recovery.ts`](../priorart/openclaw/src/agents/subagents/registry/subagent-registry-restart-recovery.ts#L133). | G | Store canonical child execution, completion, delivery outbox, and settle-wake records separately, joined by stable IDs and fenced lifecycle generation. Ambiguous ownership defers effects. | SQLite is an implementation option, not schema policy; OpenClaw session-key encoding is not portable. |
| Lifecycle/restart recovery | [`main-session-restart-recovery-checkpoint.ts`](../priorart/openclaw/src/agents/main-session-recovery/main-session-restart-recovery-checkpoint.ts#L46) settles an interrupted run only when session, status, abort source, and revision still match. | Y + G | On restart, replay canonical events, validate state/checkpoint digests, reacquire leases, CAS each recovery transition, and route unresolved/ambiguous work through YAML recovery edges. | Restart must not silently rerun model calls, Bashy effects, hooks, or deliveries. Product-specific recovery branches are forbidden. |

## Required neutral graph shape

```text
trigger adapter
  -> normalize event
  -> admission / policy.evaluate
  -> load or reconstruct typed state
  -> transcript.validate -> [explicit transcript.repair]
  -> [compact -> checkpoint -> accept successor by CAS]
  -> model.invoke
  -> validate structured outcome
  -> loop admission
  -> bashy.preflight -> policy.evaluate -> [approval.wait] -> bashy.execute
  -> persist result/event -> checkpoint or compact -> loop
  -> route typed outcome
  -> enqueue sink outbox -> reconcile receipt
```

Schedules, channel messages, operator input, queued steering, child completion, and restart recovery enter through trigger adapters. They do not receive separate privileged loops. Loop-carried state is schema-typed and revisioned; every externally visible effect is preceded by an append-only intent/outbox event and followed by a typed settlement event.

## Priority changes to the Sprint 106 contract

1. **P0 — Make authorization explicit.** Require authenticated trigger facts, `policy.evaluate`, default-deny unknown evidence, and exact approval bindings to the Bashy intent/config/state/generation.
2. **P0 — Make replay effect-safe.** Specify append-only events, CAS revisions, leases/generation fences, idempotency keys, outbox settlement, and ambiguous-effect reconciliation for Bashy, hooks, deliveries, and child agents.
3. **P0 — Preserve the sole-tool rule.** The model receives only Bashy; channel, cron, approval, compaction, and subagent functions are graph nodes/adapters, never model-callable Go tools.
4. **P1 — Expose lifecycle stages.** Add typed YAML nodes/outcomes for transcript validation/repair, queue, checkpoint, compaction successor, approval, child join, delivery reconciliation, and restart recovery.
5. **P1 — Type routed boundaries.** Triggers and sinks declare schemas, capabilities, auth references, retry/reconciliation behavior, and durable receipt types. Hook mutation must invalidate preflight and approval.
6. **P2 — Keep adapters replaceable.** Channel/gateway, timer, transcript-provider, store, and delivery implementations implement neutral interfaces; their product vocabulary never changes graph semantics.

## Explicit non-extractions

- No OpenClaw/Codex/OpenCode/OpenAI/Anthropic/channel-specific branch in the Go graph interpreter.
- No hard-coded channel admission order, cron policy, agent roster, model ladder, tool list, retry count, placement rule, hook order, or sink choice.
- No global mutable hook registry, process-local acknowledgement, in-memory-only queue, or unbounded/background fire-and-forget effect for canonical work.
- No legacy compaction marker lookup, provider transcript rewrite inside the kernel, PID-based durable identity, or implicit session-key semantics.
- No direct shell/exec tool beside Bashy and no alternate native-Go execution path for a model-requested effect.

## Acceptance tests derived from the evidence

- Replaying a completed run performs zero model, Bashy, hook, child-agent, or sink effects and yields identical typed state/checkpoint digests.
- Missing admission evidence denies; a stale approval, changed intent, hook mutation, or changed lifecycle generation forces policy reevaluation.
- Crash at reserve, execute, settle, compact, successor commit, outbox send, child completion, and restart recovery converges to one terminal or explicitly ambiguous receipt without duplicate effects.
- Transcript repair never mutates original events; signed opaque content is byte-stable or rejected as a unit, and every derived replay view has a repair report and digest.
- A batch of Bashy calls is admitted atomically against one state revision; denial commits no partial reservations.
- Trigger retries and sink retries retain stable idempotency keys; uncertain delivery is reconciled rather than treated as unsent.
- Steering is acknowledged only after its event is durably accepted at a declared safe point; handoff/restart cannot acknowledge lost input.
- Parent cancellation and restart fence child execution, completion, delivery, and settle-wake state by lifecycle generation.

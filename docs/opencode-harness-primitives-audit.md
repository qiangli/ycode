# OpenCode harness primitives: extraction audit

Status: Sprint 106 story `e90f35c49593`  
Evidence baseline: OpenCode `68abdce1a092e6302e99c2821a76071ee998d8f2`  
Scope: read-only audit of `priorart/opencode`; this document is design evidence,
not approval to import its runtime or dependencies.

## Decision

OpenCode's newer core contains valuable product-neutral mechanisms for dynamic
context epochs, durable input admission, event projection, stale-call rejection,
and tool settlement. Its legacy session prompt also makes the desired agent
experience concrete, but mixes workflow, provider compatibility, client flags,
tools, and policy in one runtime loop. Bashy should extract the former as typed
mechanisms and express the latter as YAML graph stages.

Ownership labels in this audit match
[`harness-schema-design.md`](harness-schema-design.md):

- **B** — reusable Bashy/coreutils utility: deterministic OS-facing facts and
  effects behind the sole model-facing `bashy` tool.
- **Y** — YAML graph policy: ordering, routing, expressions, budgets, retries,
  limits, permissions, transforms, and failure behavior.
- **G** — neutral Go mechanism: typed state transition, durable storage,
  concurrency safety, or provider-neutral transport.

No **G** implementation may branch on `opencode`, `codex`, `openclaw`,
`hermes`, client type, provider name, model family, or compatibility generation.

## Evidence-linked extraction map

| Harness behavior | OpenCode evidence | Extract | Owner and Bashy contract |
|---|---|---|---|
| Dynamic context sources | [`SystemContext.Source` gives each typed source a stable key, codec, loader, baseline, update, and optional removal rendering](../priorart/opencode/packages/core/src/system-context/index.ts#L21) | Stable source identity; typed snapshot; explicit unavailable, unchanged, updated, replacement-ready, and replacement-blocked outcomes | **G** observes, compares, snapshots, and renders a declared source. **Y** owns source declarations, order, trust, refresh points, budgets, unavailable policy, and routes. |
| Dynamic context reconciliation | [Initialization fails on unavailable sources while refresh preserves admitted values; replacement does not silently create an incomplete baseline](../priorart/opencode/packages/core/src/system-context/index.ts#L197) | Fail-closed initialization plus deterministic delta/replacement calculation | **G** performs the typed comparison. **Y** decides whether an unavailable source blocks, preserves, skips, retries, or routes to a fallback. |
| Context epochs | [A context epoch persists baseline, snapshot, and aggregate sequence, then atomically publishes updates with projection advancement](../priorart/opencode/packages/core/src/session/context-epoch.ts#L40) | Immutable model-visible baseline ID, source snapshot, baseline sequence, config digest, and CAS update | **G** stores and advances epochs. **Y** places `context.snapshot`/`context.refresh`, declares update visibility, and routes incompatible replacements through explicit compaction/checkpoint stages. |
| Repository instructions | [Instruction discovery walks named files upward and tracks per-assistant-message claims](../priorart/opencode/packages/opencode/src/session/instruction.ts#L60) | Root-bounded deterministic discovery with provenance and duplicate suppression | Filesystem discovery can be **B**; typed claims are **G**. **Y** owns filenames, roots, symlink/trust policy, order, byte/token limits, and whether dynamically discovered files are admitted. Do not copy home-directory or compatibility defaults. |
| Durable session history | [Message and part updates publish typed events rather than mutating an in-memory transcript](../priorart/opencode/packages/opencode/src/session/session.ts#L629); [fork remaps message IDs and compaction tail references](../priorart/opencode/packages/opencode/src/session/session.ts#L691) | Typed message/part/edge records and branch provenance | **G** owns append, projection, branch, event IDs, sequence, causation, and CAS. **Y** owns retention, branch context projection, fork/join/cancel, and sinks. |
| Durable event sequencing | [The event store validates aggregate, version, sequence, ownership, duplicate IDs, and replay divergence](../priorart/opencode/packages/core/src/event.ts#L210) | Versioned append-only event envelope; atomic projection callback; strict replay | **G**. The common harness event additionally binds graph/config digest, node, attempt, state before/after, idempotency key, and redaction policy. **Y** owns retention and delivery sinks, never replay-critical filtering. |
| Compaction selection | [Compaction preserves a recent token budget and may split at a message boundary within a turn](../priorart/opencode/packages/opencode/src/session/compaction.ts#L115) | Token estimate, deterministic head/tail selection, previous-summary reference, tail cursor | Token measurement and typed selection may be **G**. **Y** owns thresholds, preserved budget/turns/content kinds, model route, prompts, failure, and checkpoint placement. |
| Compaction workflow | [The legacy processor selects history, invokes transforms, samples a compaction agent, records the summary, and may synthesize continuation](../priorart/opencode/packages/opencode/src/session/compaction.ts#L319) | Separate measure, select, summarize, checkpoint, replace, and optional continue stages | Entire order and every branch are **Y**. State replacement is **G** CAS. Never embed an `auto` compaction routine or provider-specific overflow recovery in Go. |
| Explicit loop | [The legacy loop reloads compacted history, handles queued tasks, checks overflow, samples, and routes `stop`/`compact`/`continue`](../priorart/opencode/packages/opencode/src/session/prompt.ts#L1081) | Typed `continue`, `complete`, `compact`, `blocked`, `interrupted`, and `failed` outcomes with loop-carried state | **Y** expresses the loop and all routes. **G** validates transitions and invokes stages; it does not own the loop. |
| Step cap and last step | [The configured step count marks the final step and injects a final-answer instruction](../priorart/opencode/packages/opencode/src/session/prompt.ts#L1178); [the newer runner also removes tool definitions and forces no tools on the last step](../priorart/opencode/packages/core/src/session/runner/llm.ts#L202) | Monotonic step counter and explicit last-step predicate | Counter is typed **G** state. Limit, predicate, prompt fragment, tool ceiling, and exhaustion route are **Y**. Disabling tools is redundant when only Bashy exists; the graph routes the final model stage without executable authority. |
| Task routing | [Legacy task parts dispatch either a subtask or compaction before the next provider turn](../priorart/opencode/packages/opencode/src/session/prompt.ts#L1141) | Typed task discriminator and route | **Y** switch/route. **G** may implement `agent.invoke`, durable parent-child edges, state branch, and join. A task name must resolve through YAML roster/allowlist, never a product registry. |
| Subagent lifecycle | [Task execution supports create/resume, background notification, cancellation propagation, and derived child permissions](../priorart/opencode/packages/opencode/src/tool/task.ts#L84) | Durable invoke/resume ID, child edge, bounded reservation, notification event, interrupt/join | Lifecycle primitives and monotonic authority intersection are **G**. Agent selection, depth, concurrency, placement, context projection, budget, background/join policy, and notification sink are **Y**. Feature flags and hard-coded defaults are non-extractions. |
| Tool registry | [The legacy registry hard-codes many tools and changes availability by provider, client, and flags](../priorart/opencode/packages/opencode/src/tool/registry.ts#L58); [the newer registry materializes immutable definitions and rejects stale registrations at settlement](../priorart/opencode/packages/core/src/tool/registry.ts#L50) | Retain immutable materialization identity and stale-call rejection, not the multi-tool registry | **G** validates a call against the advertised Bashy schema/digest. **B** implements Bashy operations. **Y** owns authority and call routing. The model sees exactly one tool, `bashy`; read/edit/search/task/MCP/plugin tool names are not runtime alternatives. |
| Tool settlement | [The newer runner records calls before effects, starts local calls eagerly, waits for settlement, and reloads history](../priorart/opencode/packages/core/src/session/runner/llm.ts#L69) | Durable pending/running/terminal call state; idempotency; output/artifact binding; cancellation settlement | **G** owns the state machine and concurrency guard. **B** performs the command. **Y** owns maximum parallelism, ordering, timeout, retry, aggregate outcome, and next route. |
| Bashy command utility | [OpenCode's shell path records a synthetic request, streams output, and finalizes cancellation](../priorart/opencode/packages/opencode/src/session/prompt.ts#L451) | Canonical `preflight`, `execute`, `start`, `poll`, `stdin`, `signal`, `cancel` request/result envelope with bounded output | **B**, using Bashy runner and Command Atlas facts. **Y** invokes `policy.evaluate` between preflight and effect. Host PID, environment, or temporary paths never serve as durable identity. |
| Permission evaluation | [Rules use last matching action/resource with ask as fallback](../priorart/opencode/packages/core/src/permission.ts#L76), while [pending approval waits in an in-memory map](../priorart/opencode/packages/core/src/permission.ts#L103) | Typed action/resource facts and allow/ask/deny outcome; do not copy precedence or volatile wait state | Facts and binding verification are **G/B**. Rules, precedence, fallback, scope, expiry, escalation, and outcome routes are **Y** in `policy.evaluate`. An ask creates a durable one-use checkpoint bound to intent/config/state—not an in-memory deferred. |
| Plugins and hooks | [Plugins load sequentially for deterministic registration](../priorart/opencode/packages/opencode/src/plugin/index.ts#L219) and [each named hook mutates a shared output sequentially](../priorart/opencode/packages/opencode/src/plugin/index.ts#L284) | Stable ordered dispatch and typed transform input/output | Dispatch may be **G**; commands run through **B**. **Y** declares hook event, matcher, order, sync/async mode, mutation authority, timeout, retry, failure route, and output schema. Arbitrary runtime plugin loading is not a harness mechanism. |
| Message transforms | [Messages are transformed before context assembly and model conversion](../priorart/opencode/packages/opencode/src/session/prompt.ts#L1255) | Copy-on-write typed transform with before/after digest and provenance | **G** validates transformation and records both digests. **Y** chooses transform pipeline/order and failure behavior. Transforms cannot silently rewrite already signed or snapshotted history. |
| Provider transforms | [Provider transforms contain model-family branches for thinking-prefix binding](../priorart/opencode/packages/opencode/src/provider/transform.ts#L683), schema sanitization, and reasoning options | Only a protocol adapter's capability-normalization concept | Provider adapters may be **G** compatibility boundaries selected by model declarations. All normalized results enter one internal protocol. No provider/model `switch` belongs in graph execution, policy, compaction, tools, or state. Lossy transforms emit events and may not change graph semantics. |
| Structured output | [A schema-backed terminal tool captures output exactly once](../priorart/opencode/packages/opencode/src/session/prompt.ts#L74) and [the loop fails if a finished response omitted it](../priorart/opencode/packages/opencode/src/session/prompt.ts#L1243) | Typed output schema validation and terminal success/failure outcome | Validation is **G**. **Y** declares schema, enforcement mode, repair/retry route, attempt cap, and terminal sink. Do not expose a second `StructuredOutput` model tool; the normalized model adapter returns a structured outcome while Bashy remains the sole tool. |
| Interruption | [Processor cleanup waits briefly for active calls, marks remaining calls interrupted, and completes the assistant message](../priorart/opencode/packages/opencode/src/session/processor.ts#L585); [the run coordinator stops the owner and clears a coalesced wake](../priorart/opencode/packages/core/src/session/run-coordinator.ts#L94) | Durable interrupt request, ownership/fencing token, cancellation fan-out, terminal settlement, checkpoint | **G** coordinates and fences; **B** signals command jobs. **Y** defines interrupt class, propagation, grace period, compensation, checkpoint, notification, and resume route. An interrupt is an event, not merely process-local fiber cancellation. |
| Queued and steering input | [Input admission is idempotent and receives its durable aggregate sequence](../priorart/opencode/packages/core/src/session/input.ts#L41); [steers promote through a cutoff while queued prompts promote one FIFO item](../priorart/opencode/packages/core/src/session/input.ts#L245) | Durable typed input, stable ID, admission sequence, delivery class, promotion sequence, lease/ack, causation, and provenance | Storage and atomic claim/promotion are **G**. **Y** owns admission limits, `steer` versus `queue` routing, safe drain nodes, cutoff/batch/supersession, requeue/dead-letter, and trigger authentication. |
| Queue-to-loop behavior | [The newer runner prioritizes steer, promotes at safe turn boundaries, then drains queued prompts one at a time](../priorart/opencode/packages/core/src/session/runner/llm.ts#L390) | Explicit queue watermark and promotion outcome in loop-carried state | **Y** graph stages and routes. **G** reports facts and performs the declared atomic promotion. This ordering must not remain a hard-coded Go `while` loop. |
| Local run ownership | [Coordinator serializes each session, coalesces wakeups, joins active work, and interrupts its owner](../priorart/opencode/packages/core/src/session/run-coordinator.ts#L5) | Keyed single-owner lease/fencing, coalesced wake event, join, and cleanup | Local coordination is useful **G** prior art, but a durable lease is required for restart/multi-node safety. **Y** owns placement, lease duration/renewal, contention route, and recovery. |

## Required graph shape

An OpenCode-like profile compiles to ordinary schema nodes:

```text
authenticated trigger -> queue.enqueue
run lease -> queue.drain -> context.snapshot/refresh -> context.measure
          -> [compact.select -> model.sample -> checkpoint -> state.cas]
          -> model.sample(finalStep expression, structured schema)
          -> forEach bashy call {
               bashy.preflight -> policy.evaluate
               -> [approval checkpoint -> verified resume]
               -> bashy.execute -> event.append
             }
          -> hooks/transforms -> queue.drain(steer) -> loop | complete
          -> routed sinks; release lease
```

Loop-carried typed state includes event and queue watermarks, context epoch and
snapshot IDs, graph/config digest, model-session reference, step/attempt/budget
counters, pending calls, approval bindings, child edges, lease/fencing token,
structured-output status, and checkpoint version. Each node declares every
possible outcome; expressions cannot inspect untyped maps or invoke effects.

## Explicit non-extractions

Do not copy these OpenCode choices into hidden Go orchestration:

- the legacy `while (true)` session loop or its task/compaction branch order;
- provider-, model-, client-, feature-flag-, V1/V2-, or tool-name conditionals;
- separate built-in, MCP, plugin, structured-output, read, edit, patch, task,
  question, or shell tools exposed to the model;
- last-match permission precedence, implicit ask fallback, saved approvals, or
  an in-memory pending-approval map;
- implicit last-step prompt/tool behavior, retry counts, doom-loop threshold,
  context budgets, pruning constants, or auto-continuation text;
- arbitrary plugin installation/import and mutation of live messages;
- in-process fibers/maps as proof of durable ownership, queue delivery, job
  identity, approval state, or cancellation;
- silent provider schema/message transforms or lossy history normalization.

Provider/front-end compatibility ends at adapters. Codex, OpenCode, OpenClaw,
and Hermes inputs must normalize to the same graph, typed states, Bashy
contract, policy flow, and durable events.

## OpenCode caveats that become acceptance requirements

The newer runner explicitly lists durable multi-node ownership, durable status,
stale-work rejection, bounded retries/repeated calls, cancellation settlement,
and durable continuation recovery as unfinished work
([source](../priorart/opencode/packages/core/src/session/runner/llm.ts#L43)).
Therefore Bashy must not claim these properties merely by adopting OpenCode's
interfaces. Golden traces must prove:

1. replay reproduces context epochs, queue promotion, step/last-step decision,
   transforms, permissions, structured outcome, and terminal routing;
2. a crash between recorded intent and effect settlement cannot duplicate the
   effect, and stale tool materializations fail closed;
3. stale lease, approval, interrupt, state, or context-epoch versions cannot
   resume work;
4. queue admission is idempotent and FIFO within the declared partition, while
   steer precedence occurs only at YAML-declared safe points;
5. cancellation settles every provider turn, Bashy job, tool call, child, lock,
   and checkpoint exactly once;
6. structured output is validated without adding a second model-visible tool;
7. hooks/transforms cannot escalate authority or mutate signed history without
   a new versioned event;
8. changing context, compaction, step, task, permission, queue, hook, transform,
   or sink behavior requires YAML changes only—not Go recompilation.

## Extraction priority

1. **P0:** durable event/CAS/checkpoint and session ownership/fencing contract.
2. **P0:** durable input admission/promotion with YAML-declared queue and steer
   drain points; interruption with complete terminal settlement.
3. **P0:** Bashy preflight -> `policy.evaluate` -> approval checkpoint -> bound
   execute flow, plus effect idempotency and stale-call rejection.
4. **P1:** dynamic context epochs with typed unavailable/update/replacement
   outcomes and root-bounded instruction discovery.
5. **P1:** typed step cap/last-step and structured-output validation/repair
   expressed as graph routes.
6. **P1:** compaction, subagent task routing, hooks, and transforms as explicit
   stages with no legacy runtime branches.

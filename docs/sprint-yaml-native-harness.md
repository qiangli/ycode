# YAML-Native Harness Replatform

## Goal

Replace ycode's policy-heavy runtime with a thin interpreter for one strict
`agent.yaml`. The document configures the harness itself as well as its agent
roster: ordered stages, prompt inputs, memory, models, the agentic loop, Bashy
execution, human review, persistence, frontends and telemetry.

The model sees one tool, `bashy`. Every action is a Bashy script executed by an
embedded Bashy engine. Go owns only schema validation, typed stage execution,
provider protocols, durable events and correctness/security invariants. It must
not choose an undeclared prompt section, tool, retry, fallback, loop limit,
memory policy or approval policy.

## Configuration contract

`apiVersion: ycode.dev/v1alpha1` and `kind: Harness` identify the first format.
The top-level sections are `metadata`, `harness`, `providers`, `models`,
`context`, `input`, `memory`, `bashy`, `hitl`, `sessions`, `pipelines`, `agents`,
`frontends` and `observability`.

The loader rejects unknown and duplicate keys, missing references, invalid
stage ordering, unbounded loops and cycles outside `repeat`. It supports only
`${ENV}` / `${ENV:-default}` scalar substitution and `file:` content sources.
Content files cannot add configuration. Relative files resolve from the YAML
directory and remain inside declared readable roots. Resolved secrets never
appear in dumps, events, errors or telemetry.

There are no runtime behavior defaults. The shipped example is complete and is
the source of recommended policy. `examples/agent.yaml` is not a toy: it is the
canonical self-hosting definition of ycode itself. The binary's ordinary
one-shot, interactive and service entrypoints must load that document and may
not assemble an equivalent pipeline in Go.

## Prior-art reconstruction audit

The language is judged by whether `agent.yaml` plus reusable Bashy commands can
reconstruct and behaviorally impersonate representative harnesses, not by
whether it exposes all of ycode's old knobs. Ycode remains a neutral graph
interpreter: an impersonation must not add a Codex/OpenCode/OpenClaw/Hermes
branch to Go. Four
read-only case studies in `priorart/` set the initial expressiveness floor:

| Harness | Behavior the YAML language must express |
|---|---|
| Codex | pre-turn compaction; incremental context fragments; queued steering at safe boundaries; lifecycle hooks; turn-scoped provider sessions and bounded stream retry; parallel tool calls; sandbox/approval; subagents; exact event replay |
| OpenCode | durable prompt admission; dynamic system/context assembly; model/tool resolution per turn; step caps and last-step instructions; subtask and compaction task routing; interrupt finalization; structured output; plugin transforms |
| OpenClaw | channel/gateway admission; attempt/fallback routing; before/after tool hooks; tool-result repair; background and long-running commands; delivery sinks; scheduled/triggered turns; subagent sessions; compaction successors |
| Hermes Agent | provider-normalized messages; deterministic call IDs; duplicate-call filtering; sequential or bounded-concurrent tool execution; dangerous-command approval; steering; auxiliary-model compression with recovery; durable session branches; delegate caps |

These systems do not justify hidden Go policy. They require additional explicit
composition primitives: named pipeline calls, `switch`, bounded `retry`,
bounded `forEach` with declared concurrency and ordering, interrupt/checkpoint
boundaries, hook pipelines, queue-drain stages, structured-output validation,
and explicit error routing. Each primitive is syntax interpreted by the kernel;
the selected policies and order remain in YAML.

The [Q3 2026 agentic-engineering field report](https://x.com/DavidOndrej1/status/2094424967345496191)
adds an operational axis the four codebases alone understate. The document must also declare priority/FIFO input
queues with pre-send, lifecycle states, local/remote/worktree placement,
credential-pool and quota-aware model routes, operator-selected subagent
model/permissions/budget, skills and presets as reusable subgraphs, scheduled
or event-triggered admission, delivery sinks, pre-tool guardrail hooks and a
named global mutation lock. These are optional resources referenced by graph
nodes; ycode must never infer them from a vendor, frontend or environment.

Sprint conformance includes five complete documents: self-hosted ycode plus
Codex-like, OpenCode-like, OpenClaw-like and Hermes-like fixtures. The four
compatibility fixtures use only the portable `bashy` model tool even where the
original harness advertises specialized tools. Each becomes a complete
impersonation profile with its own context, instructions, memory, graph,
policies, roster, frontends and presentation metadata. Compatibility is proved
by differential golden event traces covering context assembly, prompt/cache
boundaries, model/tool transitions, HITL, retries, compaction, steering,
subagents, lifecycle, restart/resume and output. Product-specific wire
protocols or pixel-identical UI are included only when that profile explicitly
declares them; unsupported differences must be reported, never approximated.

The extraction work is tracked independently so evidence precedes API design:
Codex `1ba0a93865e5`, OpenCode `e90f35c49593`, OpenClaw `fce9f612a984`,
Hermes Agent `bdc739ead925`, cross-harness command-kit design `3f0923eb762b`,
Bashy/coreutils implementation `cae1f3feef40`, and ycode integration
`80fd0024965a`. Behavioral impersonation profiles and differential gates are
story `d57c126b24d8`.

## Runtime contract

Each pipeline is a dependency DAG. Nodes declare `needs`; declaration order is
only the deterministic tie-breaker. A configured concurrency bound controls
ready-node fan-out, and a node with multiple dependencies is the fan-in. The
kernel reuses the proven `coreutils/pkg/dag` graph/scheduler seam with a harness
stage executor; it does not reuse DAG.md parsing, shell task bodies, caching or
build-specific policy.

The stage catalog begins with `input.read`, `context.load`, `memory.recall`,
`prompt.assemble`, `session.append`, `llm.call`, `bashy.preflight`,
`hitl.review`, `bashy.execute`, `memory.write`, `checkpoint.save`,
`agent.invoke`, `output.emit`, plus dependency edges, `when`, `switch`, `repeat`,
`retry`, `forEach`, named pipeline calls and `fallback`.
Stages read and write named fields in typed run state. A compiled pipeline is
invalid when a stage can read a value that no predecessor produced.

The starter `turn` pipeline explicitly performs input, context, memory recall,
prompt assembly, a bounded model/tool loop, memory writeback and output. No
stage may invoke another stage implicitly.

Every model-visible input and output is reconstructable from the append-only,
schema-versioned event log. This includes context and memory injection, model
messages, Bashy calls/results, approvals, compaction and agent handoffs.

## Bashy and HITL

The portable `bashy` tool accepts `script`, `timeout_ms` and
`max_output_chars`. Its result records stdout, stderr, exit or timeout outcome,
signal, truncation and an optional spill reference. Non-zero exits and partial
timeout output are observations, not transport failures.

The Bashy story exports a stable Go runner from the sibling repository. Ycode
embeds it so the release remains one static binary. Preflight and execution use
the identical script, cwd, environment and limits.

HITL rules match Bashy's structured preflight effects, commands, paths and
network destinations and resolve to `allow`, `ask` or `deny`. `ask` persists a
checkpoint before execution. Resume accepts `approve`, `edit` or `reject`.
Approval is bound to the script and execution-context digest; an edit repeats
preflight and policy evaluation. The starter configuration denies when no
interactive frontend is available.

## Product surfaces

One-shot, REPL/TUI, HTTP/WebSocket/NATS, ACP and Go embedding all submit the
same canonical input and consume the same event stream. Named agents share
top-level resources and select pipelines by reference. Subagents use
`agent.invoke`; switching agents changes the selected roster entry rather than
changing runtime code.

The new embedding API is `Load`, `Validate`, `Run`, `Resume` and streamed
`Event`. Compatibility with the old config, wire format and Go API is not a
goal. Existing mature provider, session and memex implementations may be
adapted behind stages, but their orchestration policy must not leak through.

## Delivery waves

1. Contract: master spec, example, schema, embeddable Bashy runner.
2. Kernel: strict compiler, typed stage interpreter, event log and provider
   adapters.
3. Core behavior: Bashy execution, prompt/input, memory, HITL and the YAML
   agentic loop.
4. Product parity: roster/subagents, TUI, server transports, ACP and embedding.
5. Cutover: telemetry, conformance/evaluation, legacy deletion and docs.

Every weave issue is at most three points. Oversized work is split by provider,
stage or frontend before dispatch. Dependencies follow the wave order; work
inside a wave may proceed in parallel only when file ownership is disjoint.

## Sprint acceptance

- A fresh checkout produces one static binary with embedded Bashy.
- Editing YAML alone changes pipeline order, prompt composition, memory,
  provider/model, retry and stop policy, HITL, roster, frontends and telemetry.
- Editing YAML alone also changes graph dependencies/concurrency, input queue
  priority, execution placement, hooks/locks, triggers, sinks, skills/presets
  and subagent model/permission/budget policy.
- The model receives no tool definition other than `bashy`.
- No Go entrypoint constructs a hidden default loop or behavioral fallback.
- `examples/agent.yaml` defines ycode itself and is the configuration used by
  all ycode lifecycle conformance tests.
- Codex-like, OpenCode-like, OpenClaw-like and Hermes-like fixture documents
  compile and reproduce their audited orchestration traces with a mock model.
- Selecting one of those documents behaviorally impersonates that harness
  using the same neutral ycode kernel and only Bashy operations; a source scan
  and registry test reject product-specific runtime branches.
- Anthropic, OpenAI-compatible, Gemini and mock providers pass one canonical
  tool-call/event-order contract.
- HITL survives restart and covers allow, deny, approve, edit/re-review,
  reject, duplicate resume and stale approval.
- Replay reconstructs exact model input after tool use, compaction, subagent
  invocation and interruption.
- One-shot, TUI, server, ACP and embedding pass the same lifecycle suite.
- macOS, Linux and Windows gates pass or reject unsupported effects during
  validation.
- `bashy dag build` passes in ycode and the relevant Bashy gate passes in the
  sibling repository.
- The live tree contains no settings merge, policy-heavy conversation loop or
  specialized model-facing tool registry.

# Memory

ycode has no memory subsystem of its own. What it has is:

- **the event log as the transcript** — the append-only, hash-chained event
  store is the only conversation record;
- **`session.load` / `session.commit`** — compiled stages that give a durable
  session conversational continuity across turns, processes, and forks;
- **`context.measure` / `memory.compact`** — compiled stages that keep the
  live context inside a declared budget, with the summary produced through a
  declared route and prompts;
- **the `bashy-kb` provider** — long-term knowledge reached through the sole
  Bashy tool boundary (`bashy kb …` via harness-authored `bashy.run` nodes),
  never through an in-process store.

Every behavior below is selected in `agent.yaml`. Go implements the neutral
mechanisms; YAML owns the policy (triggers, budgets, prompts, routes, failure
behavior). The canonical wiring is [`examples/agent.yaml`](../examples/agent.yaml);
the survey that shaped this design is
[prior-art-memory-and-compaction.md](prior-art-memory-and-compaction.md).

## The event log is the transcript

`internal/harness/event` owns the durable record: append-only JSONL events,
fsynced per append, hash-chained, carrying the config digest and
session/run/stage identity. Large or model-visible bodies — including message
arrays — live in the content-addressed payload store and are referenced by
digest, so replay is complete without duplicating bytes.

There is no separate conversation database. A turn's message history is a
payload reference recorded on the event log, and every memory-relevant action
is an event:

| Event | Emitted by | Carries |
|---|---|---|
| `session.history.loaded` | `session.load` | `messages_ref`, message count, tokens, source (`turn-committed` \| `fork-seed` \| `empty`) |
| `session.turn-committed` | `session.commit` | `messages_ref`, message count, tokens, compaction count |
| `context.measured` | `context.measure` | token total and provenance (`provider` \| `estimate`, provider tokens, estimated tail, margin) |
| `memory.compacted` / `memory.compaction.skipped` | `memory.compact` | refs for input, summary, and compacted messages; preserved tokens |
| `memory.compaction.fallback_deterministic` / `.preserved` / `.failed` | `memory.compact` | the compiled `onFailure` outcome and its cause |
| `bashy.run.requested` / `bashy.run.completed` | `bashy.run` | the composed kb command, digest binding, and outcome (including `denied`) |

## Session history: `session.load` and `session.commit`

`internal/harness/stages/session` implements two stages compiled from
`spec.sessions.<ref>.history`:

- **`session.load`** runs early in the turn. It scans the session's events
  newest-first for the latest `session.turn-committed` (falling back to a
  `session.forked` parent seed, else empty), reads that message array from the
  payload store, and projects it through the compiled history policy:
  - `maxTokens` with `unit: turns` — older *whole turns* are trimmed until the
    projection fits; a turn is never split;
  - `clearToolResults: {olderThanTurns, placeholder}` — tool results older
    than N turns are replaced by the declared placeholder;
  - `repair: tool-pairs` — dangling tool calls/results are repaired before
    the first provider call, so a crashed turn cannot poison the next one.

  The projection lands in the `history` port and is emitted as
  `session.history.loaded`. An absent or unbounded history policy is a compile
  error — there is no hidden default.

- **`session.commit`** runs at the turn boundary, after the agent loop. It
  writes the turn's full message array to the payload store, appends
  `session.turn-committed`, and outputs `messagesRef`.

The consequences, pinned by tests in `stages/session` and `pkg/ycode`:

- two `Run`s on one `SessionID` are one conversation — the second turn's
  `llm.requested` carries the first turn's messages;
- history survives a new `Harness` over the same control root (a process
  restart continues the conversation);
- a forked child (`Harness.Fork`) carries the parent's history through the
  `session.forked` parent seed;
- the REPL and every other frontend get continuity from the same two stages —
  there is no frontend-private transcript.

`prompt.assemble` places history by declared order — the canonical turn uses
`[context, knowledge, history, input]` — and records what it assembled in
`prompt.assembled`.

## Compaction

Compaction is a graph decision, not a background routine. The compiled shape,
from `spec.memories.<ref>.compaction`:

```yaml
memories:
  main:
    provider: bashy-kb
    compaction:
      preserveRecentTokens: 24000
      preserveUserMessagesTokens: 4000
      reserveTokens: 8000
      routeRef: main
      promptSourceRef: compaction-checkpoint
      updatePromptSourceRef: compaction-update
      onFailure: fallback-deterministic
```

**Measurement first.** `context.measure` runs before each model call. Tokens
are the last provider-reported usage in the transcript plus a counted estimate
of the tail appended since — never a flat guess over the whole transcript —
and the provenance (source, provider tokens, estimated tail, safety margin) is
recorded in `context.measured`. The trigger is window-relative: the context
budget derives from the route's model window minus `reserveTokens` (the output
reserve); there is no hard-coded threshold literal.

**The compaction pipeline** (`compact-state` in the canonical example) is
ordinary graph:

1. `checkpoint.save` — a durable boundary before anything is rewritten;
2. `messages.clear-tool-results` — tool results older than the declared turn
   count are replaced with the declared placeholder;
3. `memory.compact` — the summary step.

**The summary is a user-role message.** `memory.compact` cuts the transcript
at a pair-safe boundary that preserves `preserveRecentTokens` of recent
messages and `preserveUserMessagesTokens` of user text, sends the head through
the compiled route (`routeRef`) with the declared prompt source
(`promptSourceRef`; `updatePromptSourceRef` performs a rolling merge when a
previous summary exists), and replaces the head with one user-role message
prefixed `[compaction-summary]`. Prompts are YAML sources; the mechanism
supplies none.

**Failure is compiled, not improvised.** `onFailure: fallback-deterministic`
produces a deterministic, no-model summary (and emits
`memory.compaction.fallback_deterministic`); `preserve` keeps the transcript
untouched; `fail` fails the stage. On provider context overflow the canonical
example makes exactly one bounded retry: a `fallback` form whose second
attempt is `compact-and-call-model`.

## Knowledge: `bashy.run` and the `bashy-kb` provider

`provider: bashy-kb` is the only compiled memory provider. The harness opens
no store: recall and persist are harness-authored **`bashy.run`** nodes —
ordinary Bashy commands that follow the same preflight → YAML policy →
digest-bound execution path as a model tool call, and fail closed on a
non-`allow` decision.

**Recall** runs `bashy kb context --json` with the compiled memory policy
substituted as flags:

```yaml
script: "bashy kb context --json --for {{task}} --rings {{memory.rings}}
         --forms {{memory.forms}} --budget {{memory.budget}} --k {{memory.k}}"
```

`{{memory.*}}` placeholders resolve at **compile** time from the node's
`memoryRef` (`--budget` = `recall.maxTokens`, `--k` = `recall.maxItems`), so
the flags cannot drift from the YAML policy. `{{task}}` and the reserved
`{{session}}` resolve at **stage** time as single-quoted shell literals,
before preflight — never as shell variable expansions, which the intent
analyzer cannot prove — so the digest-bound authorization covers the fully
composed command.

The stdout envelope is a frozen cross-repo contract
(`internal/harness/stages/memory/testdata/kb-context-envelope.json`,
byte-identical to the umbrella golden): `blocks[]{ring, form, ref, tokens,
text}`, `budget{limit, used}`, `abstained`, `rings[]{name, ok, error?}`. The
decoded blocks become the `knowledge` prompt port, and each block keeps its
`ring`/`form`/`ref` provenance in `prompt.assembled`.

**Persist** runs once per turn (`write.everyTurns: 1` is the only accepted
value until a session turn counter exists):

```yaml
script: "bashy kb note add --candidate --ring agent --episode {{session}} --json"
```

Runtime writes are always `--candidate` in the agent ring, keyed to the
session as the episode; promotion to `validated` happens outside the harness,
through kb's gate-verdict path.

**Degradation is a YAML decision.** When the kb call is denied or fails, the
canonical example's `fallback` forms substitute an honest abstained envelope
(`{"abstained":true,"blocks":[]}`) for recall and a skip marker for persist.
The denial itself stays on the event log as a `bashy.run.completed` outcome.

## What memex was, and where it went

`pkg/memex` was ycode's previous in-process memory subsystem: KV
(bbolt) / SQLite / Bleve / vector stores with fused retrieval, entity
extraction, persona modeling, layered "context defense," and background
consolidation. Earlier revisions of this document described that system; the
YAML-native harness removed every runtime path into it. `Load` opens no memex
store and creates no memex directory under the control root (pinned by test);
the dedicated recall/write stage pair is gone from the stage catalog, along
with the materializer seam and the background scheduler.

Its role is replaced by `bashy kb`: long-term records live in kb rings
(`agent` for the owning principal, `repo` committed with the code, `host` for
the machine), reached only through the governed Bashy boundary above.
Existing memex stores migrate with `bashy kb transfer --from memex`
(sprint 163 C8), which converts records into kb format in the agent ring.
`pkg/memex` remains in-tree, unreferenced by the harness, until that
migration has run on live stores; it must not be re-plugged into the runtime.

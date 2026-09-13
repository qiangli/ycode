# Prior art: transcript, compaction, and memory in shipping harnesses

Status: sprint 163 story Y5. This note replaces the nine
`gap-analysis-*-memory.md` files. Those compared a wider set of frameworks
(aider, autogen, LangGraph, MetaGPT, …) against the pre-rewrite ycode runtime;
their "ycode" columns described the deleted memex subsystem (7-tier stores,
persona, dreaming), so keeping them was worse than deleting them. What
remains load-bearing is the focused survey of five shipping coding harnesses
— **Codex, OpenCode, OpenClaw, Hermes, open-claude-code** — whose common
denominator is what ycode's `session.load` / `session.commit` /
`memory.compact` stages implement. The survey inventory lives in the umbrella
(`docs/sprint-163-handoff.md` §Inventory); deeper per-harness mechanism
audits are in this repo:
[codex-harness-primitives-audit.md](codex-harness-primitives-audit.md),
[opencode-harness-primitives-audit.md](opencode-harness-primitives-audit.md),
[openclaw-harness-primitives-audit.md](openclaw-harness-primitives-audit.md),
[hermes-harness-primitives-audit.md](hermes-harness-primitives-audit.md).

## Survey: the five harnesses across eight dimensions

| Dimension | Common pattern across the five | Variations and exceptions |
|---|---|---|
| **Transcript store** | Append-only transcript; the write path never rewrites history in place | Codex records rollouts as append-only JSONL with pending-suffix ordering protection; OpenCode appends typed message/part events with strict sequence validation; none uses a mutable conversation table as the source of truth |
| **Load at turn** | Read-side projection: the model-visible history is derived at turn start from the newest compaction boundary forward, not replayed in full | Transcript repair runs before the first provider call (dangling tool calls/results are fixed or placeholdered); OpenCode additionally prunes stale tool output on load |
| **Compaction shape** | Summary replaces the head of the transcript as a **user/contextual message with a handoff prefix**; the tail after the boundary is preserved verbatim | Rolling merge of the prior summary into the next one in 3 of 4 implementations surveyed with a summary path; OpenCode can split at a message boundary inside a turn; Codex runs compaction both pre-sampling and mid-loop |
| **Resume** | Reopening a session = load the projection from the newest boundary; no harness replays the raw event stream into the prompt | Repair-before-first-call is what makes resume after a crash safe everywhere |
| **Fork** | New session id + copied or referenced history + a parent pointer | OpenCode remaps message ids and compaction tail references on fork; Codex filters which rollout items survive into the child |
| **Memory write** | Long-term memory is **decoupled from compaction** — compaction never doubles as the knowledge-write path | Explicit in Codex (two-phase extract/consolidate pipeline with locks and watermarks) and Hermes; curated memory is injected as a budgeted system-prompt snapshot, episodic memory is reached via tools; secrets are redacted and memory content is treated as data, not instructions |
| **Token measurement** | Tokens = **last provider-reported count + `chars/4` estimate of the tail** appended since; trigger is window-relative with an explicit output reserve | Pre-turn check plus a bounded overflow retry is the universal recovery shape; OpenCode replays the last user message if compaction itself overflows |
| **Summary prompts** | The compaction prompt is a first-class, versioned prompt asset with a handoff prefix, distinct from the agent's system prompt | Implementations with rolling merge carry a second "update" prompt that folds the previous summary into the new one |

Two negative findings matter as much as the table:

- **None of the five builds a repo map or a code graph.** Code intelligence
  as recall (the kb `code` form) goes past all of them.
- **None treats the transcript as a knowledge store.** The event stream stays
  a stream; long-term records are written deliberately, through a separate
  door.

## What ycode implements from this

| Survey finding | ycode mechanism |
|---|---|
| Append-only transcript | the event log itself: hash-chained JSONL events + content-addressed payload refs (`internal/harness/event`) |
| Projection from the newest boundary | `session.load`: newest `session.turn-committed` (or fork seed) → history policy projection (`maxTokens` in whole turns, `clearToolResults`, `repair: tool-pairs`) |
| Turn boundary write | `session.commit` → `session.turn-committed` with `messages_ref` |
| Summary as user-role handoff message | `memory.compact`: `[compaction-summary]`-prefixed user message from `promptSourceRef`, rolling merge via `updatePromptSourceRef` |
| Provider count + estimated tail | `context.measure` with provenance recorded in `context.measured` |
| Window-relative trigger + output reserve | context budget = route model window − `reserveTokens`; no threshold literal |
| Bounded overflow retry | `fallback` form: `call-model` → `compact-and-call-model` on `context-overflow`, one retry |
| Fork = new id + referenced history + parent pointer | `Harness.Fork` → `session.forked` event; `session.load` fork-seed source |
| Memory decoupled from compaction | the `bashy-kb` provider: recall/persist are `bashy.run` kb nodes, entirely separate from `memory.compact` |
| Memory content treated as data | kb blocks enter the prompt as a typed `knowledge` port with `ring`/`form`/`ref` provenance, never as instructions |

See [memory.md](memory.md) for the full description of these stages.

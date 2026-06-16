---
name: conductor
description: Goal-oriented orchestrator — direct a team of agent CLIs (weave/loom) through plan → research → fan-out → steer → converge → retro, looping until a goal's contract is verified done
executor: cnl
phase: orchestrate
user_invocable: true
aliases: [director, orchestrator]
---

# /conductor — Goal-Oriented Orchestrator

`/conductor` is the **director**: it sits one level above the
single-purpose orchestration skills and drives a *team* of agent CLIs
(claude, codex, gemini, opencode, aider) to achieve a goal, looping
until the goal is verifiably done.

It does not replace `/weave`, `/foreman`, or `/autopilot` — it
**delegates** to them. Each of those is one loop shape:

| skill | shape |
|---|---|
| `/autopilot` | one agent, inline, autonomous (research→plan→build→test→fix→commit) |
| `/foreman` | one Worker at a time, backlog-driven, via Loom |
| `/weave` | N parallel agent CLIs over a queue, isolated sandboxes |

`/conductor` is the unifying spine of all three — *decompose → isolate →
gate → converge* — expressed as a **dhnt skill**: the **goal is the
contract**, the phases are steps, and "until done" is "loop until the
contract holds." That makes a run *attestable*: a dhnt-aware runtime
executes the canonical form at the bottom of this file and emits a
verifiable receipt; any other tool just follows the prose.

## The contract is the spine

A conductor run is **valid** iff three checks hold (see Success criteria):

1. **goal-met** — the goal verifier exits 0 (e.g. `go test ./...`, a
   custom gate, or — by default — "all queued work merged"). In `--judge`
   mode this is a model verdict instead of an exit code.
2. **converged** — no open/unmerged work remains in the queue.
3. **reviewed** — an *independent post-convergence review* of the merged
   result passes (a regression gate, so a merged combination that breaks
   the tree is caught before accept; defaults to re-running the goal
   verifier, override with `--review`).

This is the leveler: every executor tier (a strong model, a weak model,
a deterministic runner) is judged by the *same* contract, so results
converge regardless of who does the work. "Goal-oriented until done"
means you re-run conductor until all three checks pass — the language has
no loop construct on purpose; the goal contract *is* the loop condition.

### On-fail policy and the learn loop

The skill declares `onifaili balocakeroso` (**on-fail: blockers**): an
unmet goal does **not** crash the run — conductor surfaces the failing
checks as blockers and exits gracefully (exit 0). Each run makes
progress; you re-invoke to continue. (This is the weave "blocker-escape"
discipline, now a first-class dhnt policy. The language also offers
`aboroto` = abort and `retoroyu` = retry for other skills.)

`--adapt` turns the re-run into a **self-healing learn loop**: conductor
prefers the host's previously-learned orchestration for this environment,
and on failure (with `--repair-agent`) a model proposes a fix that is
**contract-verified against the original spec** and *folded* into a
host-local version — so the next run reuses it. This is the RETRO phase
made durable: lessons persist as verified skill versions
(`~/.dhnt/versions`), promotable to the catalog with `dhnt promote`.

## Effect cap (blast radius)

A conductor run may only: **read, write, net, spend, time**. It **must
not destroy** — no `git add -A`, no `rm`. Agent CLIs reach the network
and spend tokens; the fleet writes; waiting on convergence burns
wall-clock — all bounded, nothing destructive.

## The phase loop

Run `dhnt conductor` (below) — or drive the phases by hand with
`ycode weave`:

1. **PLAN** — decompose the goal into independent, disjoint-scope issues
   and file them into the queue. `ycode weave add "<title>" --priority p1`.
   Sprint-plan poker (advisory Fibonacci estimates from cheap headless
   agents) is optional here, exactly as in `/weave` Phase 1.5.
2. **RESEARCH** *(only when the goal is complex)* — if the queue has more
   than a handful of issues, research approaches/prior-art/risks first
   (hand off to `/learn` or `/web-research`). This is a branch: simple
   goals skip it. _(judgement allowed)_
3. **FAN-OUT** *(routed by parallel-safety — see Scheduling strategy)* — a
   2-way router: fan out to a **FLEET** (one agent CLI per issue, isolated
   sandboxes) **only when** the work is *many AND disjoint*; a single issue
   **or** tasks that share an implementation route to **SEQUENTIAL** — one
   worker grinding + resuming. Parallel agents on shared source produce
   competing rewrites that collide irreconcilably at merge.
4. **STEER** — watch and unblock: `ycode weave list`, `ycode weave log N`,
   inject keystrokes with `ycode weave say N "<msg>"`. _(judgement allowed)_
   Then an **ESCALATE** branch fires *only when* workers are stuck/blocked —
   it nudges them (`weave say`) to continue or write a BLOCKERS note.
5. **CONVERGE** — wait for the fleet, then merge **verified** work back:
   `ycode weave wait` then `ycode weave pull`. Re-run the goal verifier
   by hand before trusting a merge (the `/weave` Phase 7 discipline).
6. **RETRO** — capture what was learned: the tool report card (which CLI
   did well on what) and any `/learn` notes for next time. This is what
   makes conductor *self-improving* across runs, not just within one.

## Scheduling strategy

The conductor's job is **optimal scheduling of agents over tasks** — maximize
velocity per token, not just "run agents in parallel." The rules below are
earned from dogfooding and are what the FAN-OUT router encodes:

1. **Route by parallel-safety, not scale.** Fan out to a FLEET *only when*
   tasks are **many AND disjoint** (non-overlapping source). A single task,
   **or any set that shares an implementation** (one feature, overlapping
   files), runs **SEQUENTIALLY** — one worker grinding and resuming.
   *Why:* parallel agents on shared code each rewrite the same functions
   differently; the results **collide irreconcilably at merge**, so the
   parallel attempt (a whole agent's effort) *plus* conflict-resolution
   costs far more time and tokens than one agent doing it in sequence.
   A flip-in-isolation is **not** a flip-when-integrated.
2. **Assign by capability.** Put the strongest-fit agent on each task (the
   RETRO report card): deep multi-file work → the strongest model; tightly
   pinned surgical edits → a one-shot agent; verification/judging → a
   separate reviewer. Don't give exploratory work to one-shot agents.
3. **Hard single-feature task = sequential grind with resume.** Decompose
   into bite-size, commit each reduction, resume until done (e.g. 143→32→
   10→0). Agents often hit the watchdog mid-work with an *uncommitted*
   fix — recover it (commit the residue) and resume; don't discard.
4. **Race, don't merge, competing attempts.** To explore *approaches* to one
   hard problem, run agents in **separate** sandboxes and take the single
   **furthest** result — never merge two independent attempts at the same
   feature (that is the collision in #1).
5. **Gate every merge on the FULL suite, not the per-task measure.** A task
   can pass its own gate while silently breaking a sibling that shares code
   (the CONVERGE/REVIEW phases exist for exactly this — see Success criteria).

The router's `parallel-safe` check defaults to **sequential** (the safe
choice) unless it can confirm `>1 issue AND disjoint scopes`; the concrete
check lives in the Spec (runtime config) so the PLAN phase's scope
discipline drives it.

## Running it

```bash
# Drive the whole loop for a goal, verified by your own gate:
dhnt conductor . --goal "make package X cancellation-safe" --verify "go test ./..."

# Pick the roster of agent CLIs to enlist:
dhnt conductor . --goal "…" --roster claude,codex,gemini --verify "make test"

# Default verifier = "all queued work merged" (omit --verify):
dhnt conductor . --goal "triage and fix the flaky tests"

# No clean pass/fail gate? Let a model judge goal-met from the merged work:
dhnt conductor . --goal "the docs now explain the retry semantics" --judge --judge-agent gemini

# Independent review gate (a linter / smoke distinct from the goal verifier):
dhnt conductor . --goal "…" --verify "go test ./..." --review "golangci-lint run"

# Self-healing learn loop: prefer/learn a host-local orchestration variant:
dhnt conductor . --goal "…" --verify "make test" --adapt --repair-agent codex

# Bound the effort: at most 5 rounds, stop if the pooled spend exceeds a ceiling:
dhnt conductor . --goal "…" --verify "go test ./..." --max-rounds 5 \
  --budget-probe 'sh -c "test $(ycode weave cost --total) -lt 500000"'
```

### Bounding effort — `--max-rounds` and `--budget-probe`

"Goal-oriented until done" is bounded, not open-ended. `--max-rounds N`
(default 1) runs the conductor up to N rounds, stopping the moment the
contract holds — the explicit, terminating form of the loop. PLAN is
idempotent (it files the goal only if not already queued), so re-running
rounds re-checks CONVERGE/REVIEW as the fleet finishes rather than
duplicating work.

`--budget-probe "<cmd>"` is a **spend ceiling**: before each round the
command runs, and a non-zero exit ("over budget") stops the loop cleanly.
This keeps the budget honest — rather than faking token metering, it
delegates measurement to a probe you wire to a real cost source (the same
command-gate shape as the goal verifier), e.g.
`'sh -c "test $(ycode weave cost --total) -lt 500000"'`. Being over budget
is a stop condition, not a contract failure — it isn't part of the skill's
canonical form, so the conductor identity is unchanged. (`--max-rounds`
bounds the non-adapt loop; `--adapt` has its own bounded repair loop.)

### Judge mode (`--judge`)

Some goals have no exit-coded verifier — "the docs read more clearly,"
"the refactor is simpler," "the feature behaves as described." For those,
`--judge` swaps the deterministic goal-met check for a **model judge**: it
gathers evidence (a summary of the merged work plus recent history) and
asks an agent CLI whether the goal is *fully* achieved, defaulting to "not
met" when unsure. The convergence gate stays deterministic. This is a
distinct dhnt skill (`ConductorJudgeSkill`) — a different contract is a
different content address — whose only change is the goal-met clause:
`enisure meto value go` (judged) in place of `enisure exito value go`
(exit-coded). Its canonical form re-parses to identity
`h4d294d74295d462a53cbf6ba168fe835b1b02a52acd56ac86332485a976980c3`.

### Per-task contracts (P6 composition)

The fleet doesn't dispatch opaque agents — each task is a **sub-skill with
its own contract**, the dhnt analogue of CrewAI's per-task
`expected_output`: a task is done when *its* scoped tests pass and its work
is committed (`ConductorTaskSkill`, effect cap `{read, write, time}`). Because
the conductor **composes** that task skill (pillar P6) and a task's effect
cap is a subset of the conductor's, the composition is *statically
auditable*: `Library.EffectViolations(ConductorComposedSkill())` is empty —
dispatching tasks can never widen the orchestrator's blast radius — and the
dependency `Closure` lists exactly the sub-skills the fleet may call. (The
runnable conductor binds leaf primitives; the composed variant is the
analysis view.)

`dhnt conductor` runs the phases and prints the attestation
(`outcome=… valid=… consistent=… passed=… failed=… effects=…`). If
`valid=false`, read which contract check failed (`exito(value=go)` /
`meto(value=go)` = goal, `exito(value=cu)` = convergence,
`exito(value=vi)` = review) — under the blockers policy these are
reported and the run still exits 0; address them and re-invoke. That is
the "until done" loop, closed by you (or, with `--adapt`, the
self-healing Runtime).

The concrete `ycode weave` argv, the verifier, and the roster live in
the **Spec** (runtime config), so this skill stays portable and free of
free text — the same discipline that makes `git add -A` structurally
impossible in the safe-commit skill.

## Canonical form (dhnt)

For dhnt-aware runtimes — execute this; it re-parses to the same skill
and yields the identity below:

```
sokilili coniducatoro efefecato reada wurite neto sopenida time fini enisure exito value go fini enisure exito value cu fini enisure exito value vi fini sotepo sa rune value pa fini wuheni exito value bo sotepo si rune latitude judage value re fini fini wuheni exito value ni sotepo fo rune value fa fini elise sotepo so rune value lo fini fini sotepo su rune latitude judage value wo fini wuheni exito value tu sotepo ne rune latitude judage value ke fini fini sotepo ta rune value vo fini sotepo te rune value ru fini onifaili balocakeroso fini fini
```

identity: `hdff537944f4158711267c9403cf454ff3f809e8faf5a9496a5389f0ba5656b54`

> Source: this skill is authored as a runnable dhnt skill in
> `github.com/dhnt/dhnt` (`skills/dev/conductor.go`: `ConductorSkill` +
> `ConductorSpec`, driven by `dhnt conductor`). Edit it there, then
> re-export.

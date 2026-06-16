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

A conductor run is **valid** iff two checks hold (see Success criteria):

1. **goal-met** — the goal verifier exits 0 (e.g. `go test ./...`, a
   custom gate, or — by default — "all queued work merged").
2. **converged** — no open/unmerged work remains in the queue.

This is the leveler: every executor tier (a strong model, a weak model,
a deterministic runner) is judged by the *same* contract, so results
converge regardless of who does the work. "Goal-oriented until done"
means you re-run conductor until both checks pass — the language has no
loop construct on purpose; the goal contract *is* the loop condition.

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
3. **FAN-OUT** — enlist the team: one agent CLI per open issue, each in
   an isolated git-clone sandbox. `ycode weave start --issue N -- <tool>`.
4. **STEER** — watch and unblock: `ycode weave list`, `ycode weave log N`,
   inject keystrokes with `ycode weave say N "<msg>"`. _(judgement allowed)_
5. **CONVERGE** — wait for the fleet, then merge **verified** work back:
   `ycode weave wait` then `ycode weave pull`. Re-run the goal verifier
   by hand before trusting a merge (the `/weave` Phase 7 discipline).
6. **RETRO** — capture what was learned: the tool report card (which CLI
   did well on what) and any `/learn` notes for next time. This is what
   makes conductor *self-improving* across runs, not just within one.

## Running it

```bash
# Drive the whole loop for a goal, verified by your own gate:
dhnt conductor . --goal "make package X cancellation-safe" --verify "go test ./..."

# Pick the roster of agent CLIs to enlist:
dhnt conductor . --goal "…" --roster claude,codex,gemini --verify "make test"

# Default verifier = "all queued work merged" (omit --verify):
dhnt conductor . --goal "triage and fix the flaky tests"
```

`dhnt conductor` runs the phases once and prints the attestation
(`valid=… consistent=… passed=… failed=… effects=…`). If `valid=false`,
read which contract check failed (`exito(value=go)` = goal verifier,
`exito(value=cu)` = convergence) and re-run after addressing it — that
is the "until done" loop, with you (or the self-healing Runtime) closing
it.

The concrete `ycode weave` argv, the verifier, and the roster live in
the **Spec** (runtime config), so this skill stays portable and free of
free text — the same discipline that makes `git add -A` structurally
impossible in the safe-commit skill.

## Canonical form (dhnt)

For dhnt-aware runtimes — execute this; it re-parses to the same skill
and yields the identity below:

```
sokilili coniducatoro efefecato reada wurite neto sopenida time fini enisure exito value go fini enisure exito value cu fini sotepo sa rune value pa fini wuheni exito value bo sotepo si rune latitude judage value re fini fini sotepo so rune value fa fini sotepo su rune latitude judage value wo fini sotepo ta rune value vo fini sotepo te rune value ru fini fini
```

identity: `h2a3d6657e61f6fcb05f4cf59dc3928a52d9fda13d5adff4654e67dadaebe6fa6`

> Source: this skill is authored as a runnable dhnt skill in
> `github.com/dhnt/dhnt` (`skills/dev/conductor.go`: `ConductorSkill` +
> `ConductorSpec`, driven by `dhnt conductor`). Edit it there, then
> re-export.

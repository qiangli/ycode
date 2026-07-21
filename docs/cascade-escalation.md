# Cascade escalation — climbing the model ladder when the base is stuck

**Status:** implemented. See `internal/runtime/cascade` (the escalator),
`internal/cli/cascade.go` (the wiring), and `internal/cli/cascade_test.go`
(the gate: a looping base model is *observed* to escalate).

## The problem

A cascade agent (`ycode-cascade-x4`: glm → terra → sol) is supposed to be
cheap most of the time and expensive only when it has to be. What it actually
did was run the **base model for the entire session**: the loop detectors
fired, the coach flagged an unresolved loop, the run failed — all on the
cheapest rung, because nothing was wired to change the served model. A cascade
that never escalates is a base model with a longer name.

## How it works now

`internal/runtime/cascade.Escalator` consumes the stuck signals the runtime
already produces and advances the served model one rung at a time:

- **Loop → escalate immediately.** When the response loop detector or the
  tool-call loop detector fires (soft *or* hard threshold), the next turns run
  on the next rung. The detectors only trigger after several near-identical
  turns; waiting longer buys more of the same.
- **Stalls → escalate after 3 in a row.** A turn whose tool calls all failed
  (or that produced nothing) is a stall; three consecutive stalls escalate.
  Any real progress resets the count.
- **Never demote.** Once a run has needed premium help, it stays there — the
  base model already demonstrated it cannot do this task.
- **Unavailability is loud.** A rung with no credentials/quota is skipped with
  an error log; if *every* remaining rung is unavailable the run announces
  `✘ Cascade: … no escalation is available` (and emits a
  `cascade.unavailable` event) instead of silently grinding on the base.

Every switch is announced on the chrome, logged, and emitted as a
`cascade.escalate` event:

```
⇧ Cascade escalation: glm-4.6 → claude-opus-4-8 (rung 1, reason: loop)
```

Escalation survives the per-prompt runtime rebuild in interactive sessions,
and the escalated model — not the base — is what the usage tracker prices the
premium turns at.

## Configuring the ladder

Precedence, highest first:

| Source | Example |
| --- | --- |
| `YCODE_CASCADE_MODELS` env | `YCODE_CASCADE_MODELS="glm-4.6,gpt-5.2,claude-opus-4-8"` |
| `cascadeModels` in settings.json | `{"cascadeModels": ["glm-4.6", "claude-opus-4-8"]}` |
| `--model <cascade-agent>` | `--model ycode-cascade-x4` — the fleet catalog's `base` + `escalation` chain resolves to the ladder automatically (`api.ResolveCascadeLadder`) |

A ladder with fewer than two usable rungs disables escalation — the ordinary
single-model run.

## Observability

The per-turn action log (`docs/agent-run-observability.md`) records
`served_model`, `base_model`, `escalated`, and the escalation `reason`
(`loop`, `stall_x3`, …) on every turn, and the `fleet.escalation` metric
counts base→premium switches — so "did the premium model ever intervene?" is a
query, not an archaeology dig.

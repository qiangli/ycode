# Development pipeline

This is the contributor workflow for the YAML-native harness. Evidence comes
from the strict compiler, typed pipeline tests, replayable events, conformance
suite, and release gates—not from an alternate legacy runtime.

## 1. Locate the contract

Before editing behavior, identify the controlling `agent.yaml` resource and
the mechanism that interprets it. Start with:

- `examples/agent.yaml` for the executable complete contract;
- `internal/harness/spec` for parsing and cross-resource validation;
- `internal/harness/pipeline` for typed state and DAG execution;
- `internal/harness/turn` for stage registration;
- the relevant mechanism package under `internal/harness/`.

Behavioral knobs belong in YAML and the compiler. Do not add a CLI setting,
package global, transport-specific default, second tool registry, or hidden
retry/loop/fallback path.

## 2. Plan typed changes

For a new stage or resource, record:

- input/output port types and state writer rules;
- explicit `needs` ordering and any repeat/stop expression;
- the compiled resource references it consumes;
- canonical success/failure events and payload references;
- checkpoint and replay requirements;
- policy, platform, cancellation, and resource-limit behavior;
- frontend/public API projections that must remain equivalent.

A stage must be mechanical. Scheduling, policy, fallback, prompt ordering, and
failure routing come from the compiled graph.

## 3. Implement at the narrow seam

Keep responsibilities separated:

| Concern | Package |
|---|---|
| YAML contract | `internal/harness/spec` |
| DAG/state execution | `internal/harness/pipeline` |
| canonical messages/providers | `internal/harness/message`, `provider` |
| shell preflight/execution | `internal/harness/bashy` |
| input/context/output | `internal/harness/stages/ioctx` |
| memory/compaction | `internal/harness/stages/memory` |
| policy/HITL | `internal/harness/stages/hitl` |
| durable record | `internal/harness/event` |
| complete turn | `internal/harness/turn` |
| frontends | `internal/harness/frontend`, `cmd/ycode` |
| embedding API | `pkg/ycode` |

The provider may expose only the `bashy` function tool. Bashy execution must
use the same digest-bound script, cwd, environment, limits, and metadata as
preflight. An ask checkpoints before waiting; edit requires re-preflight;
duplicate/stale decisions fail closed.

## 4. Test the evidence chain

Add focused tests at the changed seam, including failure cases and race/restart
coverage where state is durable. Then run the complete harness conformance
gate:

```bash
./scripts/harness-conformance.sh
```

That gate covers compiler rejection, typed DAG execution, reconstructed
harness profiles, provider normalization, Bashy outcomes, event integrity,
payloads/checkpoints, HITL, memory, agent turns, observability, frontend parity,
public embedding, ACP lifecycle, and native PTY support where declared.

Run the repository gate:

```bash
bashy dag build
```

Useful focused targets are:

```bash
bashy dag fmtcheck
bashy dag vet
bashy dag verify-features
bashy dag compile
bashy dag test
```

Never weaken a schema, event, checkpoint, or conformance assertion merely to
make a new implementation pass. Update the contract and all consumers together
when the intended behavior genuinely changes.

## 5. Inspect replay and presentation parity

For runtime changes, verify:

- the event sequence is monotonic and its digest chain validates;
- event `config_digest` matches the compiled document;
- large/model-visible content is retrievable from its payload reference;
- cancellation, nonzero exit, signal, denial, and delivery failure remain
  distinct outcomes;
- local, network, ACP, and Go embedding surfaces see the same canonical
  transitions;
- resume continues a live checkpoint rather than rerunning entry stages;
- fork writes lineage/checkpoint state without starting a provider loop.

Optional OpenTelemetry spans may mirror execution, but events/checkpoints are
the replay source of truth.

## 6. Validate release impact

Changes that affect compilation, startup, shell execution, platform support,
or public APIs must pass the release-contract checks:

```bash
bashy dag release-check
bashy dag release-plan
bashy dag release-snapshot
```

The plan/snapshot must retain the five documented archive names and
`SHA256SUMS`. A published `vX.Y.Z-dev` candidate is tested on native Linux,
macOS, and Windows with:

```bash
YCODE_TEST_VERSION=vX.Y.Z-dev bashy dag qa
```

Only the already-tested bytes are promoted to bare `vX.Y.Z`. Do not create a
tag to test a fix and do not rebuild during promotion.

## 7. Review public artifacts

Before handing off, scan tracked documentation, examples, workflow output, and
proposed release notes for:

- absolute user paths or private hostnames;
- credentials, tokens, signed URLs, or secret environment values;
- stale commands, files, API names, or unsupported platform claims;
- links to missing repository paths;
- claims not demonstrated by an executable test or compiled contract.

Use conventional placeholder names such as `<PROVIDER_API_KEY>` in public
examples. Preserve unrelated work in the shared tree and report any gate
blocked by another in-progress lane separately.

## 8. Codify and hand off

Update the contract documentation in the same change as behavior. Report exact
files, tests, generated artifacts, and known boundaries. Do not commit or push
unless explicitly authorized.

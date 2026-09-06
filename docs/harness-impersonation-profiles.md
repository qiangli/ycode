# Behavioral harness impersonation profiles

The checked-in profiles reproduce orchestration behavior, not vendor protocols or
presentation. Each file is a complete `kind: Harness` document and runs through
the same typed pipeline interpreter:

| Profile | YAML-declared behavior |
| --- | --- |
| `codex-like.yaml` | run-scoped prompt cache boundary, steering-queue drain, deterministic retry, preflight, authorization, fencing, Bashy execution, output |
| `opencode-like.yaml` | step-scoped prompt cache boundary, context measurement, durable checkpoint, explicit compaction, model/tool loop, output |
| `openclaw-like.yaml` | checkpoint-bound fail-closed policy, human review, fenced Bashy execution, ordered after-tool audit hook, output |
| `hermes-like.yaml` | bounded parallel call normalization, ordered fan-in, budgeted subagent delegation, output |

`ycode.yaml` is the reference reconstruction and demonstrates context loading,
memory recall, prompt assembly, lifecycle transitions, model/provider events,
and the same preflight-to-output tool path.

All profiles explicitly select `bashy-run-v1`, `embedded-only` command
resolution, and the single `execute` operation. The model receives exactly one
tool named `bashy`. Provider behavior in conformance tests is injected through a
deterministic mock backend; stage handlers are neutral and selected by YAML stage
names. The profiles do not add product-specific Go dispatch.

The differential golden suite in
`internal/harness/pipeline/reconstruction_test.go` compiles every profile and
records stage, normalized provider, Bashy tool-call, retry, HITL, subagent, hook,
and lifecycle order. Its restart/resume trace reopens the durable event log,
continues its sequence and digest chain, resolves a pending approval, and
finishes the turn for every profile.

## Intentionally unsupported compatibility

These profiles do not claim byte-for-byte compatibility with upstream products:

- vendor-specific HTTP, WebSocket, RPC, or plugin wire schemas and event names;
- pixel-identical terminal/web UI, key bindings, slash commands, or interactive
  rendering details;
- hosted identity, billing, telemetry, cloud storage, marketplace, or proprietary
  plugin ecosystems;
- upstream-private cache, checkpoint, memory, or session serialization formats;
- native tool families or arbitrary host-command fallbacks outside the extracted
  Bashy command kit and the compiled Command Atlas.

Those surfaces remain adapters outside the neutral turn kernel. Compatibility is
limited deliberately to observable orchestration represented by the frozen YAML
contract and canonical event stream.

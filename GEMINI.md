# ycode contributor contract

This file, `CLAUDE.md`, and `GEMINI.md` intentionally carry the same contract.
Update all three in one change and keep them semantically identical.

## Product and authority

ycode is a pure-Go, YAML-native agent harness. `agent.yaml` is the only
authored runtime configuration and policy source. The canonical, self-hosting
example is `examples/agent.yaml`; it must compile without defaults supplied by
another settings system.

The ownership boundary is strict:

- **YAML owns policy:** command trees, CLI presentation and dispatch, agents,
  models and routes, graph topology, typed state,
  retry, budgets, queues, locks, hooks, triggers, sinks, placement, lifecycle,
  compaction, permissions and approval decisions.
- **Go owns neutral mechanisms:** strict compilation, graph interpretation,
  append-only events, checkpoints, provider protocol normalization, frontend
  transport, memory adapters, observability and lifecycle APIs.
- **Bashy owns execution:** `spec.bashy` is the sole model-visible tool. Every
  command follows preflight -> YAML policy evaluation -> digest-bound
  authorization -> execution. Sync and durable jobs use the same contract;
  host/native fallback and a second tool registry are forbidden.

Do not add product-, provider-, frontend- or agent-specific branches to the Go
kernel. Express behavioral variation in YAML and add a generic mechanism only
when the schema cannot represent it.

## Runtime flow and layout

`pkg/ycodecli/main.go` (wrapped by `cmd/ycode`, mounted by bashy as `bashy ycode`) opens the single composition root in
`pkg/ycodecli/harness_application.go`. Argument, stdin, REPL/TUI, ACP, HTTP,
WebSocket and NATS inputs are projections onto the same compiled graph and
canonical event stream.

Key paths:

- `internal/harness/spec/` — strict schema/compiler, imports, references,
  reachability, security checks and capability freeze.
- `internal/harness/pipeline/` — typed state, expressions, registry and neutral
  graph runner.
- `internal/harness/turn/` — stage registration and the configured agent loop.
- `internal/harness/message/` — provider-independent message representation.
- `internal/harness/provider/` — provider adapters constrained to the one
  Bashy tool.
- `internal/harness/bashy/` — adapter to Bashy's seven-operation, digest-bound
  execution contract.
- `internal/harness/event/` — fsynced hash-chained events, content-addressed
  payloads and atomic checkpoints.
- `internal/harness/stages/` — input/context, HITL and memory mechanisms.
- `internal/harness/agent/` — configured roster and bounded delegation.
- `internal/harness/frontend/` — local, PTY, HTTP, WebSocket and NATS
  projections; adapters do not choose routing or policy.
- `internal/harness/observe/` — lifecycle instrumentation controlled by YAML.
- `internal/harness/acp/` — durable ACP session/fork metadata.
- `pkg/ycode/harness.go` — public `Validate`, `Load`, `Run`, `Resume`, `Fork`,
  `Payload` and `Close` API.

Durability is part of the behavior, not optional logging. State transitions are
events; large/binary bodies are payload references; resume and fork bind exact
configuration digests, event sequences and checkpoints. Never create an
in-memory-only alternate path for a frontend.

## Command surface

All ordinary commands accept `--file/-f` and default to `agent.yaml`:

```bash
ycode validate --file examples/agent.yaml
ycode config --file agent.yaml show
ycode model --file agent.yaml list
ycode tools --file agent.yaml list       # exactly one model tool: bashy
ycode prompt --file agent.yaml "request"
printf '%s\n' 'request' | ycode --file agent.yaml
ycode repl --file agent.yaml
ycode --file agent.yaml                  # configured terminal/TUI frontend
ycode serve --file agent.yaml            # configured HTTP/WS/NATS only
ycode acp --config agent.yaml
ycode shell --file agent.yaml -c 'pwd'   # governed Bashy boundary
```

`config`, `model`, `tools`, `memory` and `skill` are read-only views of the
compiled document. There is no `config set/unset`, `model use`, wildcard tool
selection, mutable settings merge, `yc` built-in registry, or permission-bypass
flag. `ycode --help` is authoritative.

## Build and verification

Go 1.26+ is required. Inside the umbrella, sibling modules already exist.
Standalone clones must run `scripts/bootstrap-siblings.sh`; `.sibling-pins`
pins `sh`, `nadir`, `coreutils`, `bashy`, `filebrowser` and `readline`.

```bash
bashy dag compile          # build bin/ycode, one binary and no product variants
bashy dag test             # short race suite, priorart excluded
bashy dag build            # fmtcheck + vet + feature paths + compile + tests

# Frozen YAML harness lifecycle; defaults to count=3.
./scripts/harness-conformance.sh
YCODE_HARNESS_CONFORMANCE_COUNT=10 ./scripts/harness-conformance.sh

# Focused packages while iterating.
go test -short -race ./internal/harness/...
go test -short -race ./pkg/ycode ./pkg/ycodecli
```

Never run a repository-wide test glob that descends into `priorart/`. For a
manual full package list use `go list ./... | grep -v '/priorart/'`. Run the
focused race tests first, then `bashy dag build` before handoff.

## Release contract

Releases are described by `.goreleaser.yaml` and built by Bashy. Asset names
`ycode-<os>-<arch>.tar.gz` and `SHA256SUMS` are load-bearing fleet contracts.
The matrix is linux amd64/arm64, darwin amd64/arm64 and windows amd64.

```bash
bashy release check
bashy release plan
bashy release --snapshot
gh workflow run release.yml --ref main
```

Production uses two stages: push `vX.Y.Z-dev` to publish a prerelease, run
`YCODE_TEST_VERSION=vX.Y.Z-dev bashy dag qa` on Linux, Darwin and Windows so
the `refs/qa/vX.Y.Z/<os>` gates exist, then push bare `vX.Y.Z`.
`promote.yml` copies the tested bytes to the final release without rebuilding.
Never create a tag merely to test a release fix.

## Engineering and security rules

- `priorart/` is read-only. Never edit it or include it in build/test globs.
- Preserve the sole-source boundary: no settings file, hidden Go default,
  duplicate roster, second permission layer or specialized model tool.
- Reject unknown YAML fields, unresolved references, unreachable resources,
  unsupported effects/platforms, incomplete preflight and stale approval
  bindings. Fail closed.
- Thread dependencies and request metadata explicitly. Package-level mutable
  state and ambient authority are forbidden.
- Use structured logging; never emit prompts, credentials, approval material or
  control-root paths without the compiled redaction policy.
- Keep filesystem access inside compiled readable/writable roots. Control state
  belongs under the trusted `controlRoot` with restrictive permissions.
- Use `apply_patch` for edits, preserve unrelated work, stage named files only,
  and do not push without explicit approval.
- This repository is an umbrella submodule. Commit from inside `ycode/`, then
  update the umbrella pin separately; never commit ycode content from the
  umbrella root.

## Removed systems — do not resurrect

The former `internal/runtime/conversation`, `internal/runtime/session`,
`internal/runtime/config`, `internal/runtime/builtin`, `internal/runtime/toolexec`,
`internal/tools`, `internal/cli`, `internal/server`, `internal/service`, plugin
registry, specialized tools, settings merge, legacy `Agent` embedding API,
autoserver, wrap/pair/init mutation paths and model-selection flags are gone.
Historical plans may mention them; they are evidence, not implementation
instructions.

Cross-repository orchestration and foreign-agent launching belong to Bashy.
Inference/container engines live in shared dependencies and are reachable only
through declared Bashy operations, never as hidden ycode model tools.

## Design references

- `docs/harness-schema-design.md` — normative schema and ownership rules.
- `docs/harness-capability-map.yaml` — frozen capability/ownership inventory.
- `docs/bashy-harness-command-kit-design.md` — execution envelopes and effects.
- `docs/harness-impersonation-profiles.md` — prior-art behavior profiles.
- `docs/sprint-yaml-native-harness.md` — Sprint 106 acceptance contract.
- `docs/architecture.md`, `docs/usage.md`, `docs/pipeline.md`, `docs/release.md`
  — maintained user and operator documentation.

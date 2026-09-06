# ycode

ycode is a YAML-native agent harness for software work. One strict
`agent.yaml` declares the agents, models, provider routes, prompt context,
memory, typed stage graph, frontends, policy, observability, and runtime
limits. The same compiled graph drives the CLI, network adapters, ACP, and the
public Go API.

The model receives one function tool: `bashy`. Bashy preflights and executes
shell programs under the policy and resource ceilings compiled from YAML.
There is no second hidden tool registry or settings layer.

## Install

Download the archive for your platform from the
[latest release](https://github.com/qiangli/ycode/releases/latest):

- `ycode-linux-amd64.tar.gz`
- `ycode-linux-arm64.tar.gz`
- `ycode-darwin-amd64.tar.gz`
- `ycode-darwin-arm64.tar.gz`
- `ycode-windows-amd64.tar.gz`

Verify the selected archive against the release's `SHA256SUMS` before
extracting it. Unix archives contain `ycode`; the Windows archive contains
`ycode.exe`.

To build from source:

```bash
git clone https://github.com/qiangli/ycode.git
cd ycode
./scripts/bootstrap-siblings.sh
bashy dag build
```

This requires Go 1.26 or newer and a current Bashy executable.

## Quick start

Start from the canonical, fully annotated harness:

```bash
cp examples/agent.yaml ./agent.yaml
export OPENAI_API_KEY='<provider key>'
ycode validate --file agent.yaml
ycode doctor --file agent.yaml
ycode --file agent.yaml prompt 'explain this repository'
```

With no prompt, `ycode` selects the interactive frontend declared by the
compiled file. Other configured surfaces include:

```bash
ycode --file agent.yaml repl
ycode --file agent.yaml serve
ycode acp --config agent.yaml
ycode shell --file agent.yaml -c 'pwd'
```

Configuration is read-only at runtime. Edit `agent.yaml`, then validate it;
imperative model/tool/settings mutations are intentionally absent.

## What is enforced

- Unknown YAML keys, invalid references, type mismatches, cycles, undeclared
  defaults, and unsupported platform/effect combinations fail compilation.
- Pipeline ports and state have declared types and writer rules.
- Prompt ordering, memory budgets, retries, loop bounds, stop conditions,
  frontends, and failure routes come from the compiled file.
- Every transition is appended to a monotonic, hash-chained event log;
  larger content is stored by digest and referenced from events.
- HITL asks checkpoint before waiting. Approve/edit/reject resumes the live
  suspended graph; edited Bashy calls are preflighted again.
- Session fork derives a durable child checkpoint and event without starting
  a second model loop.

## Go embedding

```go
h, err := ycode.Load("agent.yaml")
if err != nil {
    return err
}
defer h.Close()

events, err := h.Run(ctx, ycode.RunRequest{
    SessionID: "session-1",
    RunID: "turn-1",
    TriggerRef: "interactive-input",
    FrontendRef: "embed",
    Principal: "local-user",
    IdempotencyKey: "request-1",
    Body: []byte(`{"request":"inspect the repository"}`),
})
if err != nil {
    return err
}
for event := range events {
    // Resolve content-addressed output with h.Payload(ref).
    _ = event
}
```

The public surface is `Validate`, `Load`, `Harness.Run`, `Harness.Resume`,
`Harness.Fork`, `Harness.Payload`, and `Harness.Close`. See
[usage](docs/usage.md#go-embedding) for request details.

## Verification and releases

```bash
./scripts/harness-conformance.sh
bashy dag build
bashy dag release-check
bashy dag release-plan
```

Release candidates are built once from `vX.Y.Z-dev`, tested as exact bytes on
Linux, macOS, and Windows with `bashy dag qa`, then byte-promoted under the
bare `vX.Y.Z` tag. See [the release process](docs/release.md).

## Documentation

- [Usage](docs/usage.md)
- [Architecture](docs/architecture.md)
- [Development pipeline](docs/pipeline.md)
- [agent.yaml schema design](docs/harness-schema-design.md)
- [Release process](docs/release.md)
- [Per-OS release gate](docs/per-os-release-gate.md)
- [Contributor instructions](AGENTS.md)

## Prior art

ycode is an original Go implementation informed by the permissively licensed
agent harnesses retained under `priorart/`, including Aider, Cline, Codex,
Continue, Gemini CLI, OpenClaw, OpenCode, and OpenHands. See that directory and
the repository's license checks for attribution details.

## License

MIT

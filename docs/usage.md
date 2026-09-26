# Using ycode

ycode executes the harness compiled from `agent.yaml`. CLI flags choose a file
or a declared frontend; they do not override models, tools, retries, policy, or
loop behavior.

The command tree, flags, help, completion and routes are declared in
`spec.interfaces.cli`. See [the CLI contract](cli-contract.md) for migration,
bootstrap discovery and the bounded POC compatibility statement. Offline
help uses the embedded canonical YAML when no default project file exists;
an explicitly selected missing or invalid file fails.

## Prepare and validate a harness

```bash
cp examples/agent.yaml agent.yaml
export OPENAI_API_KEY='<provider key>'

ycode schema > agent.schema.json
ycode validate --file agent.yaml
ycode config --file agent.yaml show
ycode doctor --file agent.yaml
```

`validate` rejects unknown keys and invalid cross-references, pipeline types,
cycles, lifecycle transitions, platform declarations, policies, or limits.
`config` is read-only inspection: `show`, `get <dot.path>`, and `path` are
available; `set` and `unset` are not.

The canonical file uses an OpenAI-compatible provider. The compiler/runtime
also has normalized Anthropic and Gemini adapters. Provider endpoints and
credential environment references are declared under `spec.providers`; they
are not global ycode settings.

## Run configured local frontends

Submit one prompt:

```bash
ycode --file agent.yaml prompt 'review the current changes'
# Equivalent root one-shot form:
ycode --file agent.yaml 'review the current changes'
```

Pipe stdin or start a configured interactive frontend:

```bash
printf '%s\n' 'summarize this repository' | ycode --file agent.yaml
ycode --file agent.yaml repl
ycode --file agent.yaml
```

The selected frontend reference must exist in YAML and permit that mode. Input
size, schema, identity, idempotency, agent route, HITL capability, and output
sinks are compiled controls.

Turns on one session id form one conversation: `session.load` projects the
previously committed history into each new turn's prompt under the compiled
history policy, and `session.commit` records the finished turn. Pass
`--session <id>` to continue a durable session across invocations:

```bash
ycode --file agent.yaml --session review-1 'review the current changes'
ycode --file agent.yaml --session review-1 'now fix the first finding'
```

Run a single Bashy program without a model call:

```bash
ycode shell --file agent.yaml -c 'pwd'
```

The inherited `--file` flag works before or after a subcommand. The command
is preflighted, evaluated by the default agent's policy, and executed only on
an allow decision with the same digest-bound cwd/environment/limits. There is
no interactive legacy shell and no permission bypass flag.

## Inspect compiled resources

These commands report YAML resources and do not mutate a settings database:

```bash
ycode model --file agent.yaml list
ycode tools --file agent.yaml list
ycode memory --file agent.yaml list
ycode skill --file agent.yaml list
ycode config --file agent.yaml get spec.runtime.defaultAgentRef
ycode version
```

Use each command's `--help` for its exact read-only projections.

## Network frontends and ACP

Start all declared HTTP, WebSocket, and NATS frontends:

```bash
ycode --file agent.yaml serve
```

Listener addresses, endpoints, authentication, request limits, routes, resume
support, and output delivery are taken from YAML. If no network frontend is
declared, `serve` fails rather than inventing one.

A serve command with `dispatch.scope: frontend` serves only its
`frontendRef`. Each frontend's address is printed to stderr as it starts;
address `127.0.0.1:0` lets the kernel pick a free port. On the HTTP API an
`output.emitted` event carries the delivered answer text in `output`.

An `http` frontend with `ui: chat` (bearer auth required) also serves a
built-in browser chat page on `GET /` that calls the same authenticated POST
API. Its start line is the URL to open, `http://ADDR/#token=…`: the token
rides in the fragment, which the browser never sends to the server, and the
page keeps it for the tab only. genie declares one as its `web` command:

```yaml
frontends:
  web:
    kind: http
    ui: chat
    listen: {network: tcp, address: "127.0.0.1:0"}
    tls: {mode: disabled-loopback-only}
    auth: {mode: bearer, secretRef: {provider: env, name: GENIE_WEB_TOKEN}}
    limits: {maxInputBytes: 1048576, maxConcurrent: 4}
# cli.root.commands:
- {name: web, dispatch: {operation: serve, scope: frontend, frontendRef: web, triggerRef: interactive-input, agentRef: coder}}
```

Serve Agent Client Protocol over stdio:

```bash
ycode acp --config agent.yaml
```

ACP negotiates the supported protocol strictly. New/load/resume/list/close and
fork delegate to durable harness session state. Reusing a session or restarting
the ACP process continues the conversation from the stored completed turn
boundary: the next turn's prompt carries the committed history, projected
through the compiled history policy by `session.load`, rather than starting a
parallel conversation loop. Fork records lineage and derives a child boundary
without running a turn; the child's first turn continues the parent's
conversation from the forked seed. Live approval continuation is exposed
through the Harness/controller resume path described below.

## Events and outputs

All surfaces observe the same canonical events. Useful event classes include
input admission, prompt assembly, provider streaming, Bashy request/result
(including harness-authored `bashy.run` calls), policy/HITL transitions,
session history (`session.history.loaded`, `session.turn-committed`), context
measurement (`context.measured`), compaction (`memory.compacted` and its
skipped/fallback/failure variants), output delivery, turn failure, and session
fork. Events are ordered and hash-chained and carry the compiled config
digest.

Output content may be referenced rather than inlined. Go embedders resolve a
payload reference with `Harness.Payload`. Transport adapters must project the
same referenced output and must not treat arbitrary log or display text as the
result.

## Go embedding

Validate and load:

```go
if err := ycode.Validate("agent.yaml"); err != nil {
    return err
}
h, err := ycode.Load("agent.yaml")
if err != nil {
    return err
}
defer h.Close()
```

Run and consume the durable stream:

```go
stream, err := h.Run(ctx, ycode.RunRequest{
    SessionID:      "session-1",
    RunID:          "turn-1",
    TriggerRef:     "interactive-input",
    FrontendRef:    "embed",
    Principal:      "local-user",
    IdempotencyKey: "request-1",
    HumanAvailable: true,
    Body:           []byte(`{"request":"inspect the repository"}`),
})
if err != nil {
    return err
}
for item := range stream {
    // Decode item.Data for the versioned event type. For output.emitted,
    // pass the delivery payload_ref to h.Payload.
}
```

`AgentRef` is optional when the trigger route supplies it. The other identity
and routing fields above are required.

When the stream emits `hitl.waiting`, pass its decision/version/digests back to
the live run:

```go
continued, err := h.Resume(ctx, ycode.ResumeRequest{
    SessionID: "session-1", RunID: "turn-1",
    DecisionID: decisionID, ExpectedVersion: version,
    ReviewDigest: reviewDigest, ReportDigest: reportDigest,
    Action: "approve", Actor: "reviewer",
})
```

Allowed actions come from the selected YAML policy. An edited action also
supplies `EditedCall` and triggers a fresh Bashy preflight. `Resume` requires a
live continuation; it rejects a process-restored pending stack rather than
rerunning the pipeline from its entry.

Fork at a completed event boundary:

```go
forked, err := h.Fork(ctx, ycode.ForkRequest{
    ParentSessionID: "session-1",
    SessionID:       "session-2",
    RunID:           "fork-session-2",
    AtSequence:      completedSequence,
})
```

The returned stream contains the canonical `session.forked` event. Fork checks
the parent event and turn checkpoint, then persists the child checkpoint. It
does not call the provider.

Embedding tests can override a declared provider transport with
`WithHarnessProvider(ref, provider)`. `WithHarnessTracer(tracer)` supplies the
tracer used only when the compiled observability resource enables spans.

## State and failure behavior

Private runtime state is rooted below the platform user-config directory and
the `spec.runtime.controlRoot.platformDataDir` value. It includes events,
content-addressed payloads, checkpoints, memory, Bashy authorization state,
and ACP lineage. Do not edit these files by hand.

Cancellation closes the stream and propagates the context outcome. Nonzero and
signaled Bashy processes remain structured results. Admission, provider,
policy, checkpoint, delivery, or pipeline failures emit/return typed failures;
they are never rewritten as successful assistant output.

## Verification

```bash
./scripts/harness-conformance.sh
bashy dag build
```

The conformance script repeats strict compiler, pipeline, provider, Bashy,
event, HITL, memory, frontend, public API, and ACP tests under the race
detector. Release checks and native exact-byte QA are documented in
[release.md](release.md) and [per-os-release-gate.md](per-os-release-gate.md).

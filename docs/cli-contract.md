# YAML CLI contract

The command surface is authored under `spec.interfaces.cli` in `agent.yaml`.
`internal/harness/cli` builds a fresh Cobra parser and renderer from that
compiled resource; `cmd/ycode` dispatches its typed invocation to existing
harness, inspection and transport mechanisms. There are no Go command
constructors or product-name dispatch branches.

Copy `examples/agent.yaml` to migrate a project. Existing harness documents
without `interfaces.cli` still validate through the public API, but the CLI
requires an explicit CLI resource. The canonical document is also embedded
verbatim for offline help, schema, version, docs and feature inspection when
the default project file does not exist. Configured execution still requires
the selected file. An explicitly selected missing or invalid file fails.

## Versioned surface

This POC extends `ycode.dev/v1alpha1`; identity uses `version: build`. YAML
owns names, aliases, usage, short/long help, examples, deprecation, positional
cardinality/enums, flag types/defaults/environment bindings and inherited or
local scope. `strings` flags are repeatable and use CSV parsing. Local flags
cannot shadow inherited flags, and local flags are not inherited by children.
Defaults are explicit strings parsed using the same rules as user values.
Conflicts, requirements and required flags apply to explicit and environment
values. Unknown fields, duplicate names/aliases/flags, unsafe environment
bindings and invalid references fail compilation.

Bootstrap config selectors are authored spellings, including `--file`, `-f`
and the ACP `--config`. They accept separate, equals and short joined values;
the last selector wins. `YCODE_CONFIG` supplies the optional file binding.
Values belonging to another flag and arguments after `--` cannot select a
configuration file. Bootstrap selects the compiled document before parsing
its command tree; the invocation records that document's source path.

## Input and dispatch

`input` dispatch declares typed frontend, trigger and agent references, a
payload key and a session flag. `auto` selects positional input first, then
the declared terminal frontend for a TTY, then stdin when permitted. Explicit
`args`, `stdin` and `repl` modes select their declared projections. Empty
prompt input follows the declared reject/help policy. The canonical terminal
projection remains the existing line-oriented TUI transport; the CLI adds no
alternate agent loop or presentation engine.

Every submission goes through the existing frontend controller and public
`Harness.Run`, with append-only events, session history and checkpoints.
Inspection reads the compiled document without opening execution state.
Generic operation identifiers describe mechanisms (`input`, `inspect`,
`validate`, `schema`, `shell`, `serve`, `acp`, `readiness`, `version`, `docs`,
`features`, `help`, `completion`, `unsupported`); command names select data.
Table columns for collection inspection are dot paths authored in dispatch.

`shell` follows the same preflight, policy and digest-bound Bashy execution
path. Its timeout must fit the compiled execution ceiling. Parse, policy and
child execution failures return a nonzero process status; shell-wrapper
errors are no longer swallowed before the ordinary dispatcher runs.

## Presentation and failure

Help and usage templates, help-flag spelling, error prefix and all error exit
codes are explicit. The POC supports text, JSON and table output with text as
the default, `color: never`, `quiet: false`, `redact: compiled`, and no automatic
usage dump on errors. Alternate policies fail compilation. JSON is selected
only where the command declares its `--json` flag; inspection never emits
resolved secret values. Help and completion commands are declared in YAML,
while Cobra provides completion rendering and its hidden completion protocol.

The canonical exit mapping is success `0`, usage `2`, configuration `3`,
runtime `1`, unsupported `4`, interrupted `130`. Cancellation is carried into
execution. Unsupported commands can declare `operation: unsupported` and
fail explicitly; they cannot invoke undeclared vendor behavior.

## Verification and scope

```bash
go test -short -race ./internal/harness/spec ./internal/harness/cli ./cmd/ycode
./scripts/harness-conformance.sh
bashy dag build
```

The reusable CLI fixture driver checks normalized output, invocation, error
class, exit status and completion behavior without provider calls or a
prior-art checkout. Runtime integration tests project argv/stdin/REPL/TTY
through an injected provider and verify durable session commits. The Bashy
`examples/cli/ycode/` pair validates the same canonical YAML and exercises its
thin native Bash++/fenced-Go adapter.

This is the ycode-first POC for story #31. No OpenCode, Codex, OpenClaw,
Hermes Agent or mini-SWE-agent CLI profile is delivered by this change.

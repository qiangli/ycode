# Sprint 132: YAML-defined agent CLI profiles

## Outcome

Make the command-line interface another declarative projection of the generic
harness. A single strict `spec.interfaces.cli` contract defines the visible
command tree and maps parsed invocations onto existing pipeline inputs. Go keeps
only the neutral bootstrap needed to locate and validate YAML, render the tree,
and dispatch a typed invocation.

The sprint delivers five profiles: ycode, OpenCode, Codex, OpenClaw, and Hermes
Agent. Product names select YAML documents, never product-specific Go branches.

## Ownership boundary

YAML owns:

- command and subcommand names, aliases, grouping, descriptions, examples, and
  deprecation notices;
- positional arguments, inherited and local flags, defaults explicitly written
  in the document, validation, conflicts, and environment bindings;
- help, version, completion, stdin/TTY selection, output presentation, exit-code
  mapping, and dispatch targets;
- the mapping from a parsed invocation to typed pipeline input.

Go owns:

- bootstrap flags required to locate the YAML document;
- strict schema compilation and semantic validation;
- generic parsing, help/completion rendering, typed dispatch, and error
  reporting;
- platform-neutral terminal and process mechanisms.

Go must not contain ycode, OpenCode, Codex, OpenClaw, or Hermes Agent command
branches. Vendor protocols, hosted services, branding assets, and undocumented
quirks are outside compatibility scope unless represented by an already-neutral
harness mechanism. Unsupported behavior must fail explicitly.

## Proposed schema seam

The foundation story will freeze the exact types, but the authored shape belongs
under the existing interface section:

```yaml
spec:
  interfaces:
    cli:
      identity: {name: ycode, version: build}
      bootstrap: {configFlags: [--file, -f], defaultFile: agent.yaml}
      root:
        usage: ycode [command]
        args: []
        flags: []
        commands: []
      routing: {}
      presentation: {}
      exitCodes: {}
```

No implicit policy defaults are introduced by this sketch. The compiler must
reject unknown keys, duplicate names and aliases, ambiguous routes, invalid
argument cardinality, unresolved dispatch targets, unsafe environment bindings,
and incomplete presentation or exit-code mappings.

## Stories and priority

1. **P0 / #31 — ycode:** freeze `spec.interfaces.cli`, implement the generic
   bootstrap and renderer, migrate ycode, and establish golden conformance tests.
2. **P1 / #32 — OpenCode:** author and verify the OpenCode CLI profile.
3. **P1 / #33 — Codex:** author and verify the Codex CLI profile.
4. **P1 / #34 — OpenClaw:** author and verify the OpenClaw CLI profile.
5. **P1 / #35 — Hermes Agent:** author and verify the Hermes Agent CLI profile.

Stories #32-#35 depend only on the schema and fixture harness produced by #31.
Once that seam is frozen, they are intentionally disjoint YAML/golden-fixture
lanes and should run in parallel.

## Master execution plan

### Phase A — contract gate

Complete #31 first. Freeze the schema, compiler diagnostics, generic invocation
IR, dispatch boundary, and reusable golden-test driver. The gate is a migrated
ycode CLI with no product-specific command wiring in Go.

### Phase B — four parallel profiles

Run #32-#35 concurrently. Each lane owns one profile plus its golden fixtures.
Changes to shared schema or renderer code require coordination through #31 rather
than divergent profile-local mechanisms.

### Phase C — convergence

Run strict validation over every profile, the shared CLI conformance matrix, the
focused harness race suite, and `bashy dag build`. Review the Go tree for product
names and reject any product-specific branch. Update user and architecture docs,
then close all five stories only after the common gate passes.

## Sprint acceptance

- All five YAML documents compile under one strict contract.
- Root and nested help, aliases, argument/flag parsing, stdin/TTY selection,
  dispatch, exit codes, errors, and completion have golden coverage.
- The same generic bootstrap renders and executes every profile.
- Product-specific behavior lives only in YAML and test fixtures.
- Every profile states supported compatibility and explicit unsupported cases.
- Focused race tests and `bashy dag build` pass from a clean worktree.

# Development Pipeline

The canonical process for any non-trivial fix or feature in this repository.
It exists so changes ship with verifiable evidence — tests, telemetry, and
docs — instead of "works on my machine" hand-offs. It's also the manual
mirror of the autonomous loop in
[`autonomous-loop.md`](./autonomous-loop.md): RESEARCH → PLAN → BUILD →
EVALUATE → LEARN. The human pipeline is what we run today; the autonomous
loop is what graduates feature-by-feature as each gate becomes
machine-checkable.

## The six steps

### 1. Research

Map the change before designing it. Use Explore subagents in parallel for
breadth (≤3) when the scope is uncertain or spans multiple components;
read directly when the target is known. Inspect prior art under
`priorart/` and `reference/` for solved-this-before patterns. Web search
only after local sources are exhausted.

**Output:** a punch list of file:line drop-points and existing utilities
to reuse.

### 2. Plan

Write the plan to the per-user plan directory (created by Plan Mode).
Plans are working docs — they may reference real local paths and env vars
freely. Plans **do not** ship to the public repo.

A plan must include:

- **Context** — the problem, what triggered it, the intended outcome.
- **Root causes** — file:line citations, not hand-waves.
- **Scope** — what lands now (v1) and what's deferred (v2). Resolve open
  questions via `AskUserQuestion` before approval.
- **Verification gates** — concrete pass/fail conditions per step.
- **Critical files** — the read/edit set.
- **Codification** — what gets written to `docs/` after gates pass.

End with `ExitPlanMode` to request approval. Don't ask "is this plan ok?"
in any other form.

### 3. Build / Test

Implement against the root causes. Tests at three levels:

- **Unit** — `go test -short -race ./pkg/...`. Add a test that pins each
  bug fixed; if a future refactor reintroduces the bug, this test fails.
- **Integration** — `go test -tags integration ./internal/integration/...`.
  Real services (ycode server, Gitea, and configured external endpoints).
- **End-to-end** — TUI via `teatest` and PTY (`make test-tui`,
  `make test-tui-e2e`); web via Playwright (`make test-ui`).

Internal services first only where ycode still owns them, such as Gitea.
Model serving, Podman, SearXNG, and OTEL collection are external host-layer
concerns; use bashy or configured endpoints for those workflows.

Telemetry at the same level as the test pyramid:

- Add slog records at every silent drop site.
- Add counters/histograms to `internal/telemetry/otel/instruments.go`
  (canonical place; consumers obtain handles via the `Instruments` struct
  whose pointer must be mutated in place on collector connect — see
  `internal/telemetry/otel/provider.go` and the provider-swap test).
- When adding metrics, verify local JSONL persistence and optional OTLP
  export through `observability.collectorAddr`.

### 4. Evaluate

Run aperio-replayed evals and manual smoke checks. The eval is the gate,
not just unit tests:

- `make eval-init` — aperio replay of `/init` against a recorded cassette.
- `make test-tui-e2e` — TUI scaffold streams in a PTY.
- `make test-ui` — Playwright spec against a real running server.

Telemetry sanity after exercising the changed path:

| Pillar  | Where                                | What to verify                                       |
| ------- | ------------------------------------ | ---------------------------------------------------- |
| Metrics | local OTEL JSONL or external collector | The new counters appear with non-zero values |
| Traces  | local OTEL JSONL or external collector | `ycode` service registered, spans exist for the run |
| Logs    | local OTEL JSONL or external collector | structured log records for the change land |

If telemetry is absent, check provider setup, instrument creation paths,
collector connectivity, and the chat-runtime entry points
(`InstrumentedTurnWithRecovery` is the live chat path; it must record the
same counters as `InstrumentedTurn`).

### 5. Commit

Modular commits with conventional prefixes (`fix:`, `feat:`, `test:`,
`docs:`, `chore:`, `ci:`). One concern per commit. Stage files by name.
Run the public-artifact security check (below) on staged diffs **and**
on the proposed commit message before each `git commit`.

Don't push. Push happens only on explicit user approval.

### 6. Codify

After every verification gate is green, document what worked. Update:

- `docs/pipeline.md` (this file) — refine if a step changed.
- `docs/instructions.md` — add convention notes that future contributors
  should see on first read.
- `docs/autonomous-loop.md` — keep the human pipeline ↔ autonomous loop
  mapping accurate.

Codification is not optional — the pipeline graduates only when the
result is reproducible from the docs.

## Public-artifact security

Hard rule for everything that leaves the local machine: files under
version control, **git commit messages and tag bodies**, PR/issue titles
and bodies, release notes, and any external-facing log echoed by an
automation script.

Never include:

- Project-internal env-var names. Use the conventional public name
  (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) or a placeholder
  (`<PROVIDER_API_KEY>`).
- Absolute paths revealing a username (`/Users/<name>/…`,
  `/home/<name>/…`). Use `~/` or a relative path.
- User identifiers (login names, internal email addresses, machine
  hostnames) beyond the canonical `git config user.email`.
- Internal IPs, hostnames, ports beyond the documented public defaults
  (`127.0.0.1:31415` is fine).
- Copy-pasted error logs containing user paths.
- API keys, OAuth tokens, signed URLs, JWTs — even expired/rotated.

Exempt (local-only, never pushed):

- Plan files under the per-user agent state directory.
- Auto-memory under the per-user memory directory.
- Untracked working files outside the repo tree.
- Gitignored `.env*` files.

Pre-commit and pre-PR sanitization grep:

```bash
git diff --cached | grep -nE '(/Users/|/home/[^/]+/|sk-[A-Za-z0-9_-]{20,}|gh[ps]_[A-Za-z0-9]{30,}|[A-Z][A-Z0-9_]+_API_KEY|[A-Z][A-Z0-9_]+_TOKEN)' \
  && { echo "FOUND — sanitize before committing"; exit 1; } || true
```

If a leak ships: rotate the credential, then treat history rewrite as a
separate explicitly-approved action — never silently force-push public
history.

## Where artifacts live

| Artifact                       | Path                                      |
| ------------------------------ | ----------------------------------------- |
| Plan files (working, local)    | per-user agent state directory            |
| Auto-memory (per-user)         | per-user memory directory                  |
| Eval cassettes                 | `internal/eval/<name>/testdata/`           |
| OTel instruments registry      | `internal/telemetry/otel/instruments.go`   |
| TUI integration tests          | `internal/cli/*_test.go` (build tag `integration`) |
| TUI e2e (PTY)                  | `internal/cli/e2e_test.go` (build tag `e2e`) |
| Web e2e (Playwright)           | `e2e/tests/*.spec.ts`                      |
| Server integration tests       | `internal/integration/`                    |

## Worked example

The first instantiation of this pipeline was the
`/init`-streaming-and-telemetry fix series (see `git log --grep
'pipeline'`). The pattern emerged from that work and is now the
template; further iterations refine it.

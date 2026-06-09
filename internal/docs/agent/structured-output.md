---
topic: structured-output
summary: typed JSON envelopes for tests, LSP, and commands
when: exit-code + stdout/stderr/duration matter as data, not text — you would otherwise be parsing per-framework console output
audience: agent
max_lines: 120
---

Three built-ins return typed JSON instead of free-form text, so an
agent reading their output never has to learn the per-framework
formats of `go test` vs. `pytest` vs. `jest` vs. the next thing. Each
emits a stable envelope shape; each accepts `--json` (or returns JSON
by default) so a single `jq` filter handles every framework.

Reach for these whenever the result is data — pass/fail counts,
exit codes, durations, structured diagnostics — rather than a wall
of text intended for a human terminal.

## When to use this

- You are about to invoke `go test ./... | tail` and grep for `FAIL`.
- You are about to invoke `pytest -v | grep -E '(PASSED|FAILED)'`.
- You are about to time a command with `time` and parse the
  trailing seconds line.
- You want hover info / diagnostics for one symbol without paging
  through the language server's verbose output.

In all four cases, the structured verb returns the same shape across
frameworks: `{exit, stdout, stderr, duration_ms}` plus typed fields
specific to the verb.

## Tool surface

### `yc test [--json] [--framework <fw>] [--pattern <re>] [--path <dir>]`

Auto-detects the framework from the directory (`go.mod` → go;
`pytest.ini` / `pyproject.toml` → pytest; `package.json` jest/vitest
markers; `Cargo.toml` → cargo). Forced selection via `--framework`.
With `--json` returns:

```json
{
  "framework": "go",
  "passed": 42, "failed": 1, "skipped": 0,
  "duration_ms": 8741,
  "failures": [{"name": "TestFoo/case2", "message": "..."}]
}
```

Without `--json`, prints a one-line summary. Exit code matches the
framework (0 = all passed).

### `yc lsp <hover|definition|references|symbols|diagnostics> <file>[:line[:col]] [--json] [--language <lang>]`

Drives the workspace's language server. Position is `file:line:col`
(1-indexed). `--language` overrides the detected language; useful for
files where the extension doesn't disambiguate (e.g. embedded SQL in a
`.go` file).

With `--json`, returns the typed LSP response. Without, a human-
readable rendering. Use cases:

- `yc lsp hover internal/foo.go:42:7` — what is this symbol?
- `yc lsp diagnostics internal/foo.go` — every diagnostic for one
  file (linter / type-checker output) without re-running the whole
  build.
- `yc lsp definition internal/foo.go:42:7` — jump-to-def as data.
- `yc lsp references internal/foo.go:42:7` — every reference,
  scoped through the language server (not regex).

### `yc run [--json] -- <command> [args...]`

Run an arbitrary command and return:

```json
{
  "stdout": "...", "stderr": "...",
  "exit": 0,
  "duration_ms": 1742,
  "command": "go build ./..."
}
```

Use it for build invocations, scripts, anything where the timing or
the exit code is the data you actually care about. Exit code of `yc
run` itself is always 0 for documented invocations (the wrapped
command's exit is in the envelope) so an agent loop wrapping
`$(yc run --json -- foo)` doesn't crash on a non-zero from `foo`.

## Failure modes

| Symptom | Fix |
|---|---|
| `yc test` picks the wrong framework | Pass `--framework <go\|pytest\|jest\|vitest\|cargo>` explicitly. |
| `yc lsp` returns empty | Language server not running or extension not recognized. Check `--language` and that the workspace has been opened once. |
| `yc run` envelope's stdout is empty but the command succeeded | Many tools (e.g. `go test`) write to stderr; check the `stderr` field. |
| `duration_ms` is much larger than expected | First-run cold caches; rerun for steady-state numbers. |

## Exact calls

- Run all tests, get pass/fail counts: `yc test --json | jq '.passed, .failed'`
- Forced framework: `yc test --framework pytest --path ./svc`
- Hover info: `yc lsp hover internal/agent/routes.go:42:7 --json`
- Just diagnostics for one file: `yc lsp diagnostics ./main.go`
- Find references via LSP (not grep): `yc lsp references foo.go:10:5`
- Time a build: `yc run --json -- go build ./... | jq '.exit, .duration_ms'`
- Wrap any command: `yc run -- make ci`

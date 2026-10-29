# S531 D1f: native Windows delegate investigation

Story #58 / `547aaab09855`, 2026-10-29. Candidate bashy `81e76eca`,
ycode `79b348a`. No executor production change is required by this reproduction.

## Root cause and launch correction

The D3 script invokes `C:/Users/liqia/s531-d3/bashy-81e7.exe`.
Bashy's `internal/agentos/agentos.go` `bashySelfPath()` recognizes the running
executable only as `bashy`, `bashy.real` (optionally `.exe`), or a test binary.
For a renamed candidate it falls back to `bashy` on PATH unless `BASHY_SELF`
is explicitly set. `dispatchGenieWithHandoff` uses `runSelf` to hand the bundle
to that resolved executable. Thus the outer candidate did not establish which
engine actually ran the turn. WSL's candidate was named `bashy`.

Pin the selected candidate before invoking genie, including in `d3-run.sh`
immediately after selecting `B`:

```sh
export BASHY_SELF="$B"
```

For the recorded native run this is
`BASHY_SELF=C:/Users/liqia/s531-d3/bashy-81e7.exe`. This uses the existing
explicit self-resolution contract; it does not weaken delegate policy or
change the installed executable. A general change to renamed executable
recognition belongs in Bashy, not ycode's executor.

## Red / green evidence on elephant

Both probes used the existing candidate, with `GENIE_BAR` naming a nonexistent
file inside `C:/Users/liqia/s531-d3/`, so doctor skipped bundle execution and
did not build anything:

```sh
GENIE_BAR=C:/Users/liqia/s531-d3/nonexistent.bar \
  C:/Users/liqia/s531-d3/bashy-81e7.exe genie doctor --json

GENIE_BAR=C:/Users/liqia/s531-d3/nonexistent.bar \
  BASHY_SELF=C:/Users/liqia/s531-d3/bashy-81e7.exe \
  C:/Users/liqia/s531-d3/bashy-81e7.exe genie doctor --json
```

- **Red:** JSON `bashy` was
  `C:\Users\liqia\AppData\Local\outpost\bashy.exe`.
- **Green:** JSON `bashy` was
  `C:/Users/liqia/s531-d3/bashy-81e7.exe`.

Both doctor commands exit 1 because the bundle is deliberately absent. The
evidence is the resolved executable field, not the command's exit status.
Repeating each probe with its JSON piped to
`bashy jq -e '.bashy == "C:/Users/liqia/s531-d3/bashy-81e7.exe"'`
gave `false` / exit 1 before the pin and `true` / exit 0 with the pin.

Event 358 (with `--cwd`) and event 436 (without it) contain
`BASHY_DELEGATE_ALLOW` in their intent environments, but no forced
`BASHY_DELEGATE_CALLER`. This rules out loss of the exported allow list in those
calls. The current executor's declared caller is forced even when absent from
the host environment. The native tests exercise and verify that boundary.

## Portable regression coverage

`genie_delegate_execution_test.go` replaces the Unix-only execution test and
uses the in-process delegate with an unknown vendor, never a Unix executable
or a live model. Its helper is also shared by the existing Unix timeout tests.
The governance fixture copies the embedded genie source to a temporary
directory, so the test binary needs neither a checkout nor adjacent prompt
files. Tests cover the uncontained envelope, policy decision, forced caller in
the execution intent, refused targets, empty allow lists, dynamic commands,
and refusal of `--cwd` and other guard overrides.

The portable tests passed on elephant **before any production change**. This
is a green executor baseline, not a claimed red-to-green executor patch.
The red-to-green result above is the launch resolution correction.

Verification on Dragon:

```sh
go test -short -race ./internal/harness/bashy
GOOS=windows GOARCH=amd64 go test -c ./internal/harness/bashy \
  -o /tmp/bashy-harness.test.exe
bashy outpost scp /tmp/bashy-harness.test.exe \
  liqiang@elephant.local:C:/Users/liqia/s531-d3/
```

Verification on elephant (binary only; no builds):

```sh
bashy outpost ssh liqiang@elephant.local -- \
  C:/Users/liqia/s531-d3/bashy-harness.test.exe -test.run Delegate -test.v
```

All seven selected native tests pass, including execution without containment.
No full suites were run. A new metered D3 model/delegate run was not performed;
its existing script deletes the previous workspace, and the manager owns that
live evidence. The next D3 run must carry the self pin above.

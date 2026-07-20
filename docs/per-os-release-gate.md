# Per-OS release gate

ycode follows the umbrella per-OS release gate (see the umbrella
`docs/per-os-release-gate.md`): `vX.Y.Z-dev` builds a GitHub pre-release,
standing OS pollers run `YCODE_TEST_VERSION=vX.Y.Z-dev bashy dag qa` against the
published assets, and the bare `vX.Y.Z` release byte-promotes those tested
assets after the required `refs/qa/<ver>/<os>` refs exist.

## Why a runtime gate

ycode is platform-dependent — in-process shell runner (mvdan/sh), PTY/TUI,
MCP, filesystem, and go-git all behave differently per OS. A release must be
RUN + smoke-tested on the real OS before the tag, not merely cross-compiled.
This mirrors the proven two-stage flow bashy shipped in v0.19.0.

## Pieces

### 1. `qa` smoke target (`dag.md → ### qa`)

Portable, **LLM-free** (no model call, no API spend — it runs every release on
every OS), cwd-local (`.qa/`, no `/tmp`). Reads `$YCODE_TEST_VERSION`; downloads
the host-matching `ycode-<os>-<arch>.tar.gz` from that tag, verifies it against
the published `SHA256SUMS`, extracts it, then runs three hard checks:

1. `ycode version` output contains the base version (any `-dev`/`-rc` suffix
   stripped).
2. `ycode docs` succeeds — the offline capability-prompt surface, no model call.
3. `ycode shell -c 'echo runtime-ok'` prints `runtime-ok` — the in-process
   shell-runner surface.

Prints `Results: PASS <ver> <os>/<arch>`. Uses `bashy`'s portable userland
(`bashy curl`, `bashy sha256sum`, `bashy tar`, `bashy uname`, …) so it runs
identically on linux/darwin/windows. Windows `.zip` extraction is handled by an
inline safe-zip reader (path-traversal guarded) since `bashy unzip` does not
exist.

Repo override: `YCODE_REPO` (default `qiangli/ycode`).

### 2. Two-stage release flow

- **`release.yml`** triggers on `v*-dev` (was `v*`). It builds the per-platform
  matrix, stamps the binary with the **base** version (the `-dev` suffix is
  stripped via a Normalize step → `VERSION="${tag%%-*}"`), and publishes a
  GitHub **pre-release** (the `*-*` → `--prerelease` marking). `workflow_dispatch`
  and PR-on-this-file remain as dryrun build paths (no publish). A bare `vX.Y.Z`
  tag must NOT reach this workflow — promote.yml owns it.
- **`promote.yml`** triggers on a bare `vX.Y.Z` (`v*` + `!v*-*`), gates on
  `refs/qa/<ver>/<os>` for `required_os` (default `windows`), verifies the
  `-dev` pre-release exists and is marked prerelease, then **byte-promotes** its
  assets (exact bytes copied, NO rebuild) to the official `--latest` release.

### 3. Poller wiring (steward)

The standing OS pollers that create `refs/qa/<ver>/<os>` are wired by the
steward — not in this change.

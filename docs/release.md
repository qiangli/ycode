# Release process

ycode uses a two-stage, no-rebuild release contract. A `vX.Y.Z-dev` tag builds
the candidate once. Native OS QA downloads and verifies those exact bytes. A
bare `vX.Y.Z` tag then copies the same assets into the official release.

## Artifact contract

The release contains these load-bearing archive names:

| Asset | Archive member |
|---|---|
| `ycode-linux-amd64.tar.gz` | `ycode` |
| `ycode-linux-arm64.tar.gz` | `ycode` |
| `ycode-darwin-amd64.tar.gz` | `ycode` |
| `ycode-darwin-arm64.tar.gz` | `ycode` |
| `ycode-windows-amd64.tar.gz` | `ycode.exe` |
| `SHA256SUMS` | checksums for every archive above |

Do not rename any of the five archives: fleet consumers resolve them by exact
name. Windows also uses `tar.gz` so native QA can extract it with
Bashy's own userland and does not depend on a host `unzip`.

The build is one CGO-disabled binary per target. It contains the YAML-native
harness; there are no separately published runtime, container, model, or embed
blobs in this contract.

## Before creating a tag

Run the local release checks:

```bash
bashy release check
bashy release plan
bashy release --snapshot
```

The plan must contain exactly the five targets above. The snapshot must create
the five archives, `SHA256SUMS`, and `release-ledger.json`. Run the repository
gate as well, then exercise the workflow's dry-run path:

```bash
gh workflow run release.yml --ref main
gh run watch --exit-status
```

Never create a tag merely to test a release fix.

## 1. Publish the candidate

Create and push an annotated prerelease tag:

```bash
git tag -a v0.X.Y-dev -m "v0.X.Y-dev"
git push origin v0.X.Y-dev
```

`.github/workflows/release.yml` builds the five-target matrix, stamps every
binary with the base `v0.X.Y` version, packages the archives, writes one
`SHA256SUMS`, and publishes a GitHub prerelease. Manual dispatches and pull
requests build and package but do not publish.

If the candidate build fails, leave the tag immutable. Fix on the branch,
rerun the dry-run, and cut the next version candidate.

## 2. Run native per-OS QA

On a native Linux, macOS, and Windows host, run:

```bash
YCODE_TEST_VERSION=v0.X.Y-dev bashy dag qa
```

The `qa` task downloads the host-matching archive plus `SHA256SUMS`, verifies
the archive before extraction, and runs version, help, and configured shell
smokes against the repository's absolute canonical `examples/agent.yaml`. It
sets one dummy provider credential for strict compilation, but makes no model
request and needs no Go toolchain. A successful poller creates
`refs/qa/v0.X.Y/{linux,darwin,windows}`. Promotion requires all three refs.

The QA ref is evidence for the exact candidate tag. It is not permission to
rebuild or substitute an artifact.

## 3. Byte-promote

After all three OS refs exist, create the bare tag:

```bash
git tag -a v0.X.Y -m "v0.X.Y"
git push origin v0.X.Y
```

`.github/workflows/promote.yml` then:

1. rejects anything other than exact stable semver;
2. requires the Linux, macOS, and Windows QA refs;
3. requires `v0.X.Y-dev` to be a GitHub prerelease;
4. downloads the candidate assets;
5. verifies `SHA256SUMS` and the complete five-archive set; and
6. uploads those same bytes to the official `v0.X.Y` release.

Promotion performs no compilation or packaging.

## Fleet consumers

The official GitHub release is the distribution source of truth. Fleet
consumers should select the exact archive for the host OS and architecture,
verify it against `SHA256SUMS`, and only then swap the executable. Downstream
package-manager automation is outside this byte-promotion contract.

After publishing, bump the umbrella submodule pin from the umbrella repository
using its documented submodule workflow.

## Failure handling

- Candidate build failure: fix forward and cut a new version candidate; do not
  move or replace the failed tag.
- Native QA failure: keep the candidate prerelease, fix forward, and publish a
  new candidate version. Never author the QA ref manually.
- Promotion failure before official publication: correct the workflow or
  missing evidence and rerun; the candidate assets remain the source bytes.
- Downstream package failure: the official release remains valid; rerun only
  the downstream automation.

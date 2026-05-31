# Release process

How ycode versions get tagged, built, published, and distributed. See `docs/strategy.md#versioning--releases` for the conceptual policy (semver, cadence, dual-mode workflow); this doc is the operational checklist.

## At a glance

| Trigger | Workflow | Output |
|---|---|---|
| Push tag matching `v*` | `release.yml` (full mode) | GitHub Release with binaries + SHA256SUMS |
| Manual dispatch / PR touching `release.yml` | `release.yml` (dryrun mode) | Build matrix runs and packages assets, but no Release is published |
| GitHub Release `published` event | `update-homebrew-tap.yml` | Pushes updated `Formula/ycode.rb` to `qiangli/homebrew-ycode` (if bootstrapped) |

## Cutting a release

1. **Validate.** Trigger a dryrun first — never tag to test a fix:
   ```bash
   gh workflow run release.yml --ref main
   gh run watch <run-id> --exit-status
   ```
2. **Wait for green.** Confirm both build jobs and `package` succeed; `publish release` is correctly skipped.
3. **Tag and push.** Annotated tag, message becomes the Release body's lead-in:
   ```bash
   git tag -a v0.X.Y -m "v0.X.Y — short headline + the why"
   git push origin v0.X.Y
   ```
4. **Watch the publish.** `gh run watch` on the new run; it should complete with `success` and create a Release at `https://github.com/qiangli/ycode/releases/tag/v0.X.Y`.
5. **Verify the artifact.** Download the binary, run `ycode version`, confirm it reports the new version + commit SHA.
6. **(Optional) Confirm Homebrew tap update** if the tap is bootstrapped (see below).

## Homebrew tap

The tap lives at `qiangli/homebrew-ycode` (separate repo) and is the single source of truth for the formula. On every release, `update-homebrew-tap.yml` runs `scripts/generate-homebrew-formula.sh` against the release's `SHA256SUMS` and pushes the result to `Formula/ycode.rb` in the tap. End users install with:

```bash
brew tap qiangli/ycode
brew install ycode
```

Formula source: <https://github.com/qiangli/homebrew-ycode/blob/main/Formula/ycode.rb>
Generator: [`scripts/generate-homebrew-formula.sh`](../scripts/generate-homebrew-formula.sh)

### One-time bootstrap (already done for `qiangli/homebrew-ycode`)

The current bootstrap state:
- Tap repo: <https://github.com/qiangli/homebrew-ycode> exists and is seeded with v0.1.0.
- Deploy key (SSH, write access): titled "ycode-release-bot (auto-generated)" on the tap repo. Private half stored as the `HOMEBREW_TAP_DEPLOY_KEY` secret on this repo.

To re-bootstrap on a different fork or after rotating the key:

1. **Create the tap repo** (if it doesn't exist):
   ```bash
   gh repo create <owner>/homebrew-ycode --public \
     --description "Homebrew tap for ycode"
   ```
   The repo's name *must* be `homebrew-<tap>` — Homebrew expects the prefix.

2. **Generate a single-purpose SSH deploy key** locally (no passphrase since CI is non-interactive):
   ```bash
   ssh-keygen -t ed25519 -f /tmp/ycode-tap-deploy -N "" -C "ycode-release-bot"
   ```

3. **Add the public key as a deploy key with write access** on the tap repo:
   ```bash
   gh repo deploy-key add /tmp/ycode-tap-deploy.pub \
     --repo <owner>/homebrew-ycode \
     --title "ycode-release-bot" --allow-write
   ```
   Deploy keys are scoped to a single repo, don't appear in account-wide token lists, and rotate independently of any user account — preferable to a PAT here.

4. **Add the private key as a secret** on this (ycode) repo:
   ```bash
   gh secret set HOMEBREW_TAP_DEPLOY_KEY --repo <owner>/ycode \
     --body "$(cat /tmp/ycode-tap-deploy)"
   ```

5. **Securely delete the local copies** (the secret is now in two safe places and shouldn't be on the developer's filesystem):
   ```bash
   rm -P /tmp/ycode-tap-deploy /tmp/ycode-tap-deploy.pub
   ```

6. **Trigger an initial sync** (or just a dry validation against an existing release):
   ```bash
   gh workflow run update-homebrew-tap.yml -f tag=v0.1.0
   gh run watch
   ```
   The workflow regenerates the formula, pushes it to the tap (or no-ops if already in sync), and from that point fires automatically on every `release: published` event.

Until step 4 lands, `update-homebrew-tap.yml` exits cleanly with a notice on every release — it never fails the build.

### Rotating the deploy key

```bash
# generate a new key, add it to the tap, swap the secret, then remove the old one
ssh-keygen -t ed25519 -f /tmp/ycode-tap-deploy-new -N "" -C "ycode-release-bot"
gh repo deploy-key add /tmp/ycode-tap-deploy-new.pub \
  --repo qiangli/homebrew-ycode --title "ycode-release-bot $(date +%F)" --allow-write
gh secret set HOMEBREW_TAP_DEPLOY_KEY --repo qiangli/ycode --body "$(cat /tmp/ycode-tap-deploy-new)"
# verify with a dispatch
gh workflow run update-homebrew-tap.yml -f tag=v0.1.0
# once green, list and remove the old deploy key
gh repo deploy-key list --repo qiangli/homebrew-ycode
gh repo deploy-key delete <old-key-id> --repo qiangli/homebrew-ycode
rm -P /tmp/ycode-tap-deploy-new*
```

### Adding a new platform to the formula

When the release matrix expands (e.g., `linux-arm64` lands), edit `scripts/generate-homebrew-formula.sh` to call `sha_for <new-target>` and emit the corresponding `on_*`/`on_*` block. The next release will produce a formula with the new platform automatically.

## When something fails

- **Dryrun fails on `release.yml`:** fix the workflow / build, push the fix, re-dispatch the dryrun. Do *not* cut a new tag until the dryrun is green. (See user memory: "no tagging while fixing.")
- **Tag is pushed but build fails:** the broken tag stays in history (do not force-push delete). Fix the issue, validate via dryrun, then cut the next patch (`v0.X.Y+1`).
- **Tap update fails after a successful release:** the release itself is fine — just the tap is stale. Re-trigger manually:
  ```bash
  gh workflow run update-homebrew-tap.yml -f tag=v0.X.Y
  ```

## Artifact and naming convention

| Asset | Source | Path in archive |
|---|---|---|
| `ycode-<os>-<arch>.tar.gz` | Per-platform native build with all embeds baked in | `./ycode` (executable, codesigned ad-hoc on macOS) |
| `<embed>-<os>-<arch>.gz`   | Per-embed standalone blob (consumed by `make embed-fetch`) | n/a (the blob itself) |
| `SHA256SUMS`               | `sha256sum` over every `.tar.gz` AND every `.gz` | n/a |

Embeds shipped per platform (matches `Makefile` gates + `scripts/embed-fetch.sh` per-platform applicability):

| Platform | runner | podman | vfkit | gvproxy |
|---|---|---|---|---|
| `darwin-arm64` | ✅ `ycode-runner-darwin-arm64.gz` | ✅ `podman-darwin-arm64.gz` (client-only, `-tags remote`) | ✅ `vfkit-darwin-arm64.gz` | ✅ `gvproxy-darwin-arm64.gz` |
| `linux-amd64`  | ✅ `ycode-runner-linux-amd64.gz`  | ✅ `podman-linux-amd64.gz` (native engine, no VM) | — | — |
| `linux-arm64`  | ✅ `ycode-runner-linux-arm64.gz`  | ✅ `podman-linux-arm64.gz` | — | — |

Currently shipped binaries: `ycode-linux-amd64.tar.gz`, `ycode-linux-arm64.tar.gz`, `ycode-darwin-arm64.tar.gz`. `darwin-amd64` and `windows-amd64` remain blocked on code-level fixes — see the matrix comments in `.github/workflows/release.yml`.

## Two-track build (producer / consumer)

- **CI is the canonical producer.** `release.yml` sets `BUILD_EMBEDS_FROM_SOURCE=1`, runs `make ensure-embeds`, and uploads each resulting `.gz` as a standalone Release asset. CMake + ccache cache the llama.cpp rebuild between runs.
- **Local dev is the canonical consumer.** `make build` defaults to fetching the prebuilt embeds from the latest GitHub release matching the dev's `GOOS/GOARCH` (`scripts/embed-fetch.sh`), then falls through to a source build only if the fetch produced nothing for that embed. Set `BUILD_EMBEDS_FROM_SOURCE=1` to force source.

Implication: a dev with no CMake on a non-macOS-arm64 machine still gets a fully-functional `bin/ycode` in ~30 seconds via fetch (provided a release with their platform exists). The toolchain is only required for release-pipeline runners and devs iterating on the embeds themselves.

## Escape hatch — `--use-system-binaries`

End users who already run upstream `ollama` and `podman` and prefer them over ycode's embeds can pass `--use-system-binaries` (or set `inference.useSystem: true` / `container.useSystem: true` in `settings.json`). ycode never auto-installs upstream binaries; the flag is opt-in. Document this in any user-facing install guide that suggests pre-installing ollama/podman.

## Pre-release smoke tests (do before each tag)

```bash
git pull origin main && git submodule update --init --recursive
rm -f internal/inference/runner_embed/ycode-runner.gz \
      internal/container/{podman,vfkit,gvproxy}_embed/*.gz
BUILD_EMBEDS_FROM_SOURCE=1 make build       # exercise the source path CI uses
./bin/ycode ollama serve &                   # confirm 11434 binds
sleep 3
./bin/ycode ollama list                      # exit 0 with model list (or header-only)
kill %1                                      # stop daemon
./bin/ycode podman ps                        # confirms embedded podman + machine boot
```

After the tag is published and CI uploads the embeds, sanity-check the dev fast-path on a *separate* clean clone:

```bash
git clone <repo> /tmp/ycode-smoke && cd /tmp/ycode-smoke
git submodule update --init --recursive
make build                                   # should fetch (~30s), NOT source-build
ls -lh internal/inference/runner_embed/ycode-runner.gz
./bin/ycode ollama serve                     # verifies the released runner works
```

## Cross-cutting consumers

- **outpost** and **cloudbox** fleet upgrade should consume per-platform release artifacts from the same release URLs so a single ycode release flows out to all paired hosts consistently (see `dhnt/CLAUDE.md` "Cross-cutting touchpoints" → fleet upgrade).
- Bumping the umbrella pin after a release:
  ```bash
  cd ../          # to dhnt root
  git add ycode
  git commit -m "sync: bump ycode pin to v0.X.Y"
  git push origin main
  ```

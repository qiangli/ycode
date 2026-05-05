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

The tap lives at `qiangli/homebrew-ycode` (separate repo). On every release, `update-homebrew-tap.yml` regenerates `Formula/ycode.rb` from the release's `SHA256SUMS` and pushes it to the tap. End users install with:

```bash
brew tap qiangli/ycode
brew install ycode
```

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
| `ycode-<os>-<arch>.tar.gz` | Per-platform native build | `./ycode` (executable, codesigned ad-hoc on macOS) |
| `SHA256SUMS` | `sha256sum` over all `.tar.gz` | n/a |

Currently shipped: `ycode-linux-amd64.tar.gz`, `ycode-darwin-arm64.tar.gz`. Other platforms blocked on code-level fixes — see comments in `.github/workflows/release.yml`.

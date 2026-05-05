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

### One-time bootstrap

These are explicit destructive actions that need a human:

1. **Create the tap repo:**
   ```bash
   gh repo create qiangli/homebrew-ycode --public \
     --description "Homebrew tap for ycode"
   ```
   The repo's name *must* be `homebrew-<tap>` — Homebrew expects the prefix.

2. **Mint a Personal Access Token (PAT)** with **only** the new tap repo's `contents: write` scope. Use a fine-grained PAT, not a classic one.

3. **Add the token to this repo's secrets** as `HOMEBREW_TAP_TOKEN` (Settings → Secrets and variables → Actions → New repository secret).

4. **Trigger an initial sync.** Once the token is set, dispatch the workflow against the latest release tag:
   ```bash
   gh workflow run update-homebrew-tap.yml -f tag=v0.1.0
   ```
   The workflow will regenerate the formula, push it to the tap repo, and from that point forward fire automatically on every published release.

Until step 3 lands, `update-homebrew-tap.yml` exits cleanly with a notice on every release — it never fails the build.

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

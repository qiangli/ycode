---
name: ycode
description: ycode build, test, release and deploy tasks
default: help
---

# ycode — DAG task file

The task runner for this repo is `bashy dag <target>`. Builds produce one
YAML-native `ycode` binary with no build-tag variants. Release planning,
cross-compilation, archives and checksums are declared in `.goreleaser.yaml`
and executed locally through `bashy release`.

Targets carry `Requires:` (dependency edges) and `Effects:` (declared caps).

```bash
bashy dag --list        # list available targets
bashy dag build         # full gate
bashy dag test          # unit tests
```

## Tasks

### help
Show the target list.
```bash
bashy dag --list
```

### tidy
Run go mod tidy, gofmt and vet.
```bash
./scripts/tidy.sh
```

### fmtcheck
Fail if any file is not gofmt-clean. Read-only; `tidy` is the apply step.
```bash
./scripts/fmtcheck.sh
```

### vet
Static analysis over every package in this module. Nested prior-art modules are
excluded by Go's module boundary.
Effects: exec, net, read, write
```bash
go vet ./...
```

### verify-features
Validate the feature registry: every path in internal/features/registry.yaml
must exist. This is the usual failure after moving or deleting a package.
Effects: exec, net, read, write
```bash
go test -count=1 ./internal/features/...
```

### compile
Compile bin/ycode. One binary, no tags.
Effects: exec, net, read, write
```bash
go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev) -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" \
  -o bin/ycode ./cmd/ycode/
if [ "$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi
echo "Built bin/ycode"
```

### test
Unit tests with the race detector. Nested prior-art modules are excluded by
Go's module boundary.
Effects: exec, net, read, write
```bash
go test -short -race -count=1 ./...
```

### build
The full gate: fmtcheck → vet → verify-features → compile → test.
Requires: fmtcheck vet verify-features compile test
Effects: write
```bash
echo "build: gate passed"
```

### install
Install bin/ycode into $DHNT_BIN_DIR. Compatibility shims are not installed.
Requires: build
Effects: write
```bash
dir="${DHNT_BIN_DIR:-$HOME/.local/bin}"
mkdir -p "$dir"
# Unlink before copy so the new binary lands on a fresh inode: on macOS,
# overwriting a signed Mach-O in place leaves the kernel's per-vnode cs_blob
# cache pointing at the old signature and the next exec is SIGKILLed.
rm -f "$dir/ycode"
cp bin/ycode "$dir/ycode"
if [ "$(uname)" = "Darwin" ]; then codesign -f -s - "$dir/ycode" 2>/dev/null || true; fi
echo "Installed ycode to $dir/ (shims not installed)"
```

### clean
Remove build artifacts.
Effects: destroy, write
```bash
rm -rf bin dist
```

### install-hooks
Symlink scripts/git-hooks/* into .git/hooks/.
```bash
./scripts/install-hooks.sh
```

**Release.** Cross-compilation, archives and checksums now come from
`.goreleaser.yaml` via `bashy release`.

### release-check
Validate .goreleaser.yaml, its stages and its name templates. Builds nothing.
```bash
bashy release check
```

### release-plan
Print what a release would build and package.
```bash
bashy release plan
```

### release-snapshot
Build, archive and checksum all five release targets without a tag. Produces
`dist/ycode-<os>-<arch>.tar.gz`, `SHA256SUMS` and `release-ledger.json`.
```bash
bashy release --snapshot
```

### qa
Run the native, LLM-free release gate against exact bytes published under
`$YCODE_TEST_VERSION` (for example `v0.4.0-dev`). The gate downloads this
host's archive and the shared `SHA256SUMS`, verifies before extraction, then
checks the version, help and in-process shell surfaces. All work stays in
`.qa/`. `YCODE_REPO` defaults to `qiangli/ycode`.
```bash
set -e
BASHY_EXE="${BASHY:-bashy}"
VER="${YCODE_TEST_VERSION:?set YCODE_TEST_VERSION to a published tag such as v0.4.0-dev}"
REPO="${YCODE_REPO:-qiangli/ycode}"
BASEV="${VER%%-*}"
repo_root="$PWD"
fixture="$repo_root/examples/agent.yaml"
[ -f "$fixture" ] || { echo "qa: canonical fixture missing: $fixture" >&2; exit 1; }
# Strict compilation resolves this declaration; the smoke makes no model call.
export OPENAI_API_KEY=ycode-release-qa-no-network
uname_s=$("$BASHY_EXE" uname -s)
case "$uname_s" in *[Dd]arwin*) os=darwin;; *[Ll]inux*) os=linux;; *[Ww]indows*|*[Mm][Ii][Nn][Gg]*|*[Mm][Ss][Yy][Ss]*) os=windows;; *) echo "qa: unsupported OS $uname_s" >&2; exit 1;; esac
arch=$("$BASHY_EXE" uname -m)
case "$arch" in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=amd64;; *) echo "qa: unsupported architecture $arch" >&2; exit 1;; esac
[ "$os/$arch" != windows/arm64 ] || { echo "qa: windows/arm64 is not a release target" >&2; exit 1; }
asset="ycode-${os}-${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${VER}"
d="$repo_root/.qa"
trap 'cd "$repo_root"; "$BASHY_EXE" rm -rf "$d"' EXIT
"$BASHY_EXE" mkdir -p "$d"
"$BASHY_EXE" curl -fsSL -o "$d/$asset" "$url/$asset"
"$BASHY_EXE" curl -fsSL -o "$d/SHA256SUMS" "$url/SHA256SUMS"
want=$(awk -v a="$asset" '$2==a || $2=="*"a {print $1}' "$d/SHA256SUMS")
[ -n "$want" ] || { echo "qa: $asset is absent from SHA256SUMS" >&2; exit 1; }
got=$("$BASHY_EXE" sha256sum "$d/$asset" | awk '{print $1}')
[ "$want" = "$got" ] || { echo "qa: sha256 mismatch for $asset" >&2; exit 1; }
cd "$d"; "$BASHY_EXE" tar -xzf "$asset"
bin="$PWD/ycode"; [ "$os" = windows ] && bin="$PWD/ycode.exe"
[ -f "$bin" ] || { echo "qa: archive has no ycode binary" >&2; exit 1; }
chmod +x "$bin" 2>/dev/null || true
vout=$("$bin" --file "$fixture" version 2>&1)
case "$vout" in *"$BASEV"*) ;; *) echo "qa: expected $BASEV, got $vout" >&2; exit 1;; esac
"$bin" --file "$fixture" --help >/dev/null
[ "$("$bin" shell --file "$fixture" -c pwd)" = "$repo_root/examples" ] || { echo "qa: shell workspace smoke failed" >&2; exit 1; }
echo "Results: PASS $VER $os/$arch ($asset, sha256 verified)"
```

**Tests beyond the unit suite.** Each needs setup — read the note before running.

### test-integration
Go integration tests. Requires a running server.
Effects: exec, net, read, write
```bash
go test -tags integration -v -count=1 ./internal/integration/...
```

### test-tui
Local frontend parity and interactive approval lifecycle tests.
Effects: exec, net, read, write
```bash
go test -race -count=1 ./internal/harness/frontend -run 'Test(OneShotStdinREPLAndTUIHaveCanonicalParity|InteractiveApprovalResumeUsesSameEventStream)'
```

### test-tui-e2e
Interactive frontend projection in a pseudo-terminal.
Effects: exec, net, read, write
```bash
go test -race -count=1 -timeout 60s ./internal/harness/frontend -run '^TestPTYREPLProjectsCanonicalEvents$'
```

### test-ui
Playwright browser tests. Requires a running server and npx.
```bash
cd e2e && npx playwright test
```

**Evaluation.**

### eval-contract
Contract-tier evals: deterministic, no LLM.
Effects: exec, net, read, write
```bash
go test -count=1 ./internal/eval/contract/...
```

### eval-init
Replay /init via aperio. Offline; skips if the cassette is unrecorded.
Requires: compile
Effects: exec, net, read, write
```bash
go test -count=1 -tags eval ./internal/eval/init/...
```

### bench-memory
Memory retrieval quality benchmarks. No LLM.
Effects: exec, net, read, write
```bash
go test -run XXX -bench . -benchtime 1x ./pkg/memex/...
```

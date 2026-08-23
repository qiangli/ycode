---
name: ycode
description: ycode build, test, release and deploy tasks
default: help
---

# ycode — DAG task file

The task runner for this repo, run with `bashy dag <target>`. It replaces the
Makefile, which is gone.

Two things changed with the migration, both simplifications rather than
translations:

- **One binary, no build tags.** The Makefile's `TAG_LIST` carried `sqlite`,
  `sqlite_unlock_notify` and `bindata`, and all three gate **zero files** in this
  tree today — they were Gitea's, and Gitea left with `internal/gitserver`. The
  fourth, `embed_spawn`, selected a *variant* (embedded shim vs. the symlink
  fallback `spawn_embed.Available()` already implements). Dropping the tag set
  also dissolves the Makefile's sub-make trick, which existed only because
  `$(wildcard)` expands once per invocation.
- **Releases are `bashy release`.** Cross-compilation, archives and checksums are
  a config (`.goreleaser.yaml`) rather than a matrix of `dist/%` rules.

Targets carry `Requires:` (dependency edges) and `Effects:` (declared caps).

```bash
bashy dag --list        # what `make help` showed
bashy dag build         # full gate
bashy dag test          # unit tests
```

## Tasks

### help
Show the target list.
Effects: read
```bash
bashy dag --list
```

### tidy
Run go mod tidy, gofmt and vet.
Effects: write
```bash
./scripts/tidy.sh
```

### fmtcheck
Fail if any file is not gofmt-clean. Read-only; `tidy` is the apply step.
Effects: read
```bash
./scripts/fmtcheck.sh
```

### vet
Static analysis over every package except priorart/.
Effects: read
```bash
go vet $(go list ./... | grep -v '/priorart/')
```

### verify-features
Validate the feature registry: every path in internal/features/registry.yaml
must exist. This is the usual failure after moving or deleting a package.
Effects: read
```bash
go test -count=1 ./internal/features/...
```

### compile
Compile bin/ycode. One binary, no tags.
Effects: write
```bash
go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev) -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" \
  -o bin/ycode ./cmd/ycode/
if [ "$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi
echo "Built bin/ycode"
```

### test
Unit tests with the race detector, excluding priorart/.
Effects: read
```bash
go test -short -race -count=1 $(go list ./... | grep -v '/priorart/')
```

### build
The full gate: fmtcheck → vet → verify-features → compile → test.
Requires: fmtcheck vet verify-features compile test
Effects: write
```bash
echo "build: gate passed"
```

### install
Install bin/ycode into $DHNT_BIN_DIR. Shims are deliberately NOT installed —
routing podman/docker/bash through ycode hijacks any tool that polls them.
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
Effects: write
```bash
rm -rf bin dist
```

### install-hooks
Symlink scripts/git-hooks/* into .git/hooks/.
Effects: write
```bash
./scripts/install-hooks.sh
```

**Release.** Cross-compilation, archives and checksums now come from
`.goreleaser.yaml` via `bashy release`.

### release-check
Validate .goreleaser.yaml, its stages and its name templates. Builds nothing.
Effects: read
```bash
bashy release check
```

### release-plan
Print what a release would build and package.
Effects: read
```bash
bashy release plan
```

### release-snapshot
Build, archive and checksum every platform without a tag. Produces
dist/ycode-<os>-<arch>.tar.gz, SHA256SUMS and release-ledger.json.
Effects: write
```bash
bashy release --snapshot
```

**Tests beyond the unit suite.** Each needs setup — read the note before running.

### test-integration
Go integration tests. Requires a running server.
Effects: read net
```bash
go test -tags integration -v -count=1 ./internal/integration/...
```

### test-tui
TUI integration tests (direct Update + teatest lifecycle).
Effects: read
```bash
go test -tags integration -count=1 -timeout 60s ./internal/cli/...
```

### test-tui-e2e
TUI end-to-end in a PTY. Needs a compiled binary and a real terminal.
Requires: compile
Effects: read
```bash
go test -tags e2e -count=1 -timeout 120s ./internal/cli/...
```

### test-ui
Playwright browser tests. Requires a running server and npx.
Effects: read net
```bash
cd e2e && npx playwright test
```

**Evaluation.**

### eval-contract
Contract-tier evals: deterministic, no LLM.
Effects: read
```bash
go test -count=1 ./internal/eval/contract/...
```

### eval-init
Replay /init via aperio. Offline; skips if the cassette is unrecorded.
Requires: compile
Effects: read
```bash
go test -count=1 -tags eval ./internal/eval/init/...
```

### bench-memory
Memory retrieval quality benchmarks. No LLM.
Effects: read
```bash
go test -run XXX -bench . -benchtime 1x ./pkg/memex/...
```

**CI.**

### ci-image
Build the ycode-builder image used by the containerized matrix.
Effects: write net
```bash
${DOCKER:-podman} build -t ycode-builder .
```

### ci
Run the containerized matrix locally. Slow, definitive. Needs a container
engine; set YCODE_CI_HOST to run it on another machine instead. Bind-mounts
the worktree, its pinned sibling modules, and any external git-common-dir at
their real host paths — see scripts/ci-run.sh for why.
Requires: ci-image
Effects: write net
```bash
./scripts/ci-run.sh
```

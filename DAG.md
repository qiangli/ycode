---
name: ycode
description: Pure Go CLI agent harness build/test/eval targets as a bashy dag pipeline
default: help
---

# ycode — DAG task file

The agent-first equivalent of this repo's `Makefile`, runnable with the
`bashy dag` task runner:

```bash
bashy dag --list            # what `make help` showed
bashy dag build             # build binary with quality gates
bashy dag test              # run unit tests
```

Targets carry `Requires:` (dependency edges), `Sources:`/`Generates:` (content-fingerprint up-to-date skips), and `Effects:`/`Tools:` annotations.

## Tasks

### help
Show this list of targets and help.
```bash
bashy dag --list
```

### init
Initialize submodules, fetch plugins, and prepare embedded assets.
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
git submodule init 2>&1 | grep -v 'already registered' || true
git submodule update --recursive 2>&1 | grep -v '^From \|^Submodule path\| \* branch' || true
./scripts/fetch-perses-plugins.sh
./scripts/gzip-embeds.sh
./scripts/build-gitea-frontend.sh
echo "Generating Gitea bindata..."
cd external/gitea && go run build/generate-bindata.go options modules/options/bindata.dat 2>&1
cd external/gitea && go run build/generate-bindata.go templates modules/templates/bindata.dat 2>&1
cd external/gitea && go run build/generate-bindata.go public modules/public/bindata.dat 2>&1
cd external/gitea && go run build/generate-bindata.go modules/migration/schemas modules/migration/bindata.dat 2>&1
```

### sync
Pull latest changes for all submodules (skips on conflict).
Effects: write
```bash
./scripts/sync-submodules.sh
```

### priorart-list
List all priorart repos with branch and latest commit.
Effects: read
```bash
./scripts/sync-priorart.sh list
```

### priorart-sync
Pull latest changes for all priorart repos.
Effects: write
```bash
./scripts/sync-priorart.sh sync
```

### spawn-embed
Build + compress the ycode-spawn micro shim for embedding.
Sources: cmd/ycode-spawn/
Generates: internal/runtime/wrap/spawn_embed/ycode-spawn.gz
Effects: write
```bash
./scripts/embed-spawn.sh
```

### runner-build-if-missing
Idempotent wrapper to build or fetch Ollama inference runner.
Generates: ../coreutils/external/ollama/runner_embed/ycode-runner.gz
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
if [ ! -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  if [ -z "${BUILD_EMBEDS_FROM_SOURCE:-}" ]; then
    ./scripts/embed-fetch.sh runner
  fi
fi
if [ ! -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  ./scripts/build-runner-thin.sh
fi
```

### ensure-embeds
Ensure all embedded binaries are built/fetched.
Requires: spawn-embed, runner-build-if-missing
```bash
echo "Embeds ensured"
```

### _compile-inner
Internal compilation target.
Sources: cmd/ycode/, internal/
Generates: bin/ycode
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
mkdir -p bin
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"

TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

go build -trimpath -tags "${TAG_LIST}" -ldflags "${LDFLAGS}" -o bin/ycode ./cmd/ycode/
echo "Built bin/ycode (tags: ${TAG_LIST})"
```

### compile
Compile the ycode binary to bin/ (no checks, ensuring embeds first).
Requires: ensure-embeds, _compile-inner
```bash
echo "compile complete"
```

### verify-features
Verify the feature registry structure (paths exist, no malformed entries).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -count=1 ./internal/features/...
```

### compile-full
Alias for compile, with codesign on macOS.
Requires: compile
```bash
if [ "$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi
```

### compile-debug
Compile with debug symbols (for profiling/debugging).
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
mkdir -p bin
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"

TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

go build -trimpath -tags "${TAG_LIST}" -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/ycode ./cmd/ycode/
if [ "$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi
```

### build
Build with full quality gate: tidy -> fmt -> vet -> compile -> test -> verify.
Requires: ensure-embeds
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
PACKAGES="${PACKAGES:-$(go list ./... | grep -v '/priorart/')}"

TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

export VERSION COMMIT PACKAGES TAG_LIST
./scripts/build.sh
```

### test
Run unit tests with race detector (-short flag).
Effects: read
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
PACKAGES="${PACKAGES:-$(go list ./... | grep -v '/priorart/')}"
TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

go test -short -race -tags "${TAG_LIST}" ${PACKAGES}
```

### test-integration
Run Go integration tests (requires running server).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags integration -v -count=1 ./internal/integration/...
```

### test-container
Run container integration tests (requires podman).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags integration -race -count=1 -timeout 180s -v ./internal/container/...
```

### test-release-smoke
Fast e2e: ollama pull/run + podman build/pull/run (gates releases).
Effects: read
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

go test -tags "${TAG_LIST},release_smoke,embed_runner" -count=1 -timeout 600s -v ./internal/integration/ -run 'TestReleaseSmoke_'
```

### test-oci
Run OCI self-build integration test (requires podman).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags integration -race -count=1 -timeout 600s -v ./internal/container/... -run TestOCIBuildSelf
```

### test-gitserver
Run git server workspace integration tests.
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags "integration,sqlite,sqlite_unlock_notify,bindata" -race -count=1 -timeout 240s -v ./internal/gitserver/...
```

### test-ui
Run Playwright browser tests (requires running server + npx).
Effects: read
```bash
cd e2e && npx playwright test
```

### test-tui
Run TUI integration tests (direct Update + teatest lifecycle).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags integration -count=1 -timeout 60s ./internal/cli/...
```

### test-tui-e2e
Run TUI E2E tests in a PTY (requires compiled binary).
Requires: compile
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags e2e -count=1 -timeout 120s ./internal/cli/...
```

### test-tui-fuzz
Run TUI fuzz tests for 30s each.
Effects: read
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -run='^$' -fuzz=FuzzToolDetail -fuzztime=30s ./internal/cli/
go test -run='^$' -fuzz=FuzzTUIUpdate -fuzztime=30s ./internal/cli/
```

### test-all
Run all tests: unit + container + gitserver + TUI + integration + browser.
Requires: test, test-container, test-gitserver, test-tui, test-tui-e2e, test-integration, test-ui
```bash
echo "all tests passed"
```

### eval-agentsmd
Validate AGENTS.md quality (static analysis, no LLM).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -v -run TestAnalyze -race ./internal/eval/agentsmd/...
```

### bench-init
Run /init E2E benchmark.
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags benchmark -count=1 -timeout 35m -v ./internal/eval/benchmark/...
```

### eval-contract
Run contract-tier evals (no LLM, deterministic, fast).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -short -race ./internal/eval/...
```

### eval-smoke
Run smoke-tier evals (real LLM, pass@k, requires provider).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags eval -count=1 -timeout 5m ./internal/eval/smoke/...
```

### eval-behavioral
Run behavioral evals (trajectory analysis, requires provider).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags eval_behavioral -count=1 -timeout 30m ./internal/eval/behavioral/...
```

### eval-e2e
Run E2E evals (full coding tasks, requires provider).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -tags eval_e2e -count=1 -timeout 45m ./internal/eval/e2e/...
```

### eval-init
Replay /init via aperio (offline; skips if cassette unrecorded).
Requires: compile
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -count=1 -timeout 120s ./internal/eval/init/...
```

### eval-all-evals
Run all eval tiers.
Requires: eval-contract, eval-smoke, eval-behavioral, eval-e2e, eval-init
```bash
echo "all evaluation tiers completed"
```

### bench-memory
Memory retrieval quality benchmarks (no LLM, fast).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -run TestBenchmark -v -count=1 ./internal/runtime/memory/...
```

### bench-memory-quality
Comprehensive memory quality (large corpus, context metrics).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -run 'TestBenchmark_Quality|TestContextMetrics' -v -count=1 -timeout 2m ./internal/runtime/memory/...
```

### bench-memory-competitive
Competitive benchmark (LoCoMo subset, fusion ablation, latency).
Effects: read
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -run TestCompetitive -v -count=1 -timeout 5m ./internal/runtime/memory/...
```

### bench-memory-latency
Memory and storage operation latency benchmarks.
Effects: read
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
go test -bench BenchmarkRecall -benchmem -count=3 ./internal/runtime/memory/...
go test -bench 'BenchmarkBleve|BenchmarkVector' -benchmem -count=3 ./internal/storage/...
```

### bench-memory-all
All memory benchmarks.
Requires: bench-memory, bench-memory-quality, bench-memory-competitive, bench-memory-latency
```bash
echo "all memory benchmarks completed"
```

### vet
Run static analysis.
Effects: read
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
PACKAGES="${PACKAGES:-$(go list ./... | grep -v '/priorart/')}"
TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

go vet -tags "${TAG_LIST}" ${PACKAGES}
```

### tidy
Run mod tidy, fmt, and vet.
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
export PACKAGES="${PACKAGES:-$(go list ./... | grep -v '/priorart/')}"
./scripts/tidy.sh
```

### clean
Remove build artifacts.
Effects: destroy
```bash
rm -rf bin/ dist/
```

### install
Install the ycode binary to ~/bin/ (no shims).
Requires: build
Effects: write
```bash
set -e
mkdir -p ~/bin
rm -f ~/bin/ycode
cp bin/ycode ~/bin/ycode
if [ "$(uname)" = "Darwin" ]; then codesign -f -s - ~/bin/ycode 2>/dev/null || true; fi
echo "Installed ycode to ~/bin/"
```

### all
Full quality gate (alias for build).
Requires: build
```bash
echo "all complete"
```

### chrome-extension
Build ycode and print ycode-live Chrome extension setup.
Requires: compile
```bash
set -e
echo "ycode binary built. The ycode-live Chrome extension is embedded inside it."
echo ""
echo "Extract the extension:"
echo "  bin/ycode browser setup live"
echo ""
echo "Load it into Chrome:"
echo "  1. Open chrome://extensions"
echo "  2. Toggle Developer mode"
echo "  3. Click 'Load unpacked' -> point at bin/ycode's output"
```

### ci-image
Build the ycode-builder Docker image used by GitHub Actions.
Effects: write
```bash
set -e
DOCKER="${DOCKER:-$(command -v docker 2>/dev/null || command -v podman)}"
if [ -z "${DOCKER}" ]; then
  echo "neither docker nor podman found in PATH" >&2
  exit 1
fi
CI_IMAGE="${CI_IMAGE:-ycode-builder}"
${DOCKER} build -t "${CI_IMAGE}" .
```

### ci
Run the full GitHub Actions matrix locally (Docker).
Requires: ci-image
Effects: write
```bash
set -e
DOCKER="${DOCKER:-$(command -v docker 2>/dev/null || command -v podman)}"
CI_IMAGE="${CI_IMAGE:-ycode-builder}"
${DOCKER} run --rm "${CI_IMAGE}" make compile
${DOCKER} run --rm "${CI_IMAGE}" make vet
${DOCKER} run --rm "${CI_IMAGE}" make test
${DOCKER} run --rm "${CI_IMAGE}" make verify-features
${DOCKER} run --rm "${CI_IMAGE}" go test -short -race ./internal/features/...
${DOCKER} run --rm "${CI_IMAGE}" make test-tui
${DOCKER} run --rm "${CI_IMAGE}" make test-tui-e2e
echo "=== CI parity PASSED ==="
```

### ci-fast
Run only the verify-features + unit-test subset in Docker.
Effects: write
```bash
set -e
DOCKER="${DOCKER:-$(command -v docker 2>/dev/null || command -v podman)}"
if [ -z "${DOCKER}" ]; then
  echo "neither docker nor podman found in PATH" >&2
  exit 1
fi
CI_IMAGE="${CI_IMAGE:-ycode-builder}"
${DOCKER} run --rm "${CI_IMAGE}" make verify-features
${DOCKER} run --rm "${CI_IMAGE}" go test -short -race ./internal/features/...
echo "=== ci-fast PASSED ==="
```

### install-hooks
Symlink scripts/git-hooks/* into .git/hooks/.
Effects: write
```bash
./scripts/install-hooks.sh
```

### dist/ycode-linux-amd64
Cross-compile for Linux amd64.
Generates: dist/ycode-linux-amd64
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"
TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

mkdir -p dist
GOOS=linux GOARCH=amd64 go build -trimpath -tags "${TAG_LIST}" -ldflags "${LDFLAGS}" -o dist/ycode-linux-amd64 ./cmd/ycode/
```

### dist/ycode-linux-arm64
Cross-compile for Linux arm64.
Generates: dist/ycode-linux-arm64
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"
TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

mkdir -p dist
GOOS=linux GOARCH=arm64 go build -trimpath -tags "${TAG_LIST}" -ldflags "${LDFLAGS}" -o dist/ycode-linux-arm64 ./cmd/ycode/
```

### dist/ycode-darwin-amd64
Cross-compile for macOS amd64.
Generates: dist/ycode-darwin-amd64
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"
TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

mkdir -p dist
GOOS=darwin GOARCH=amd64 go build -trimpath -tags "${TAG_LIST}" -ldflags "${LDFLAGS}" -o dist/ycode-darwin-amd64 ./cmd/ycode/
codesign -f -s - dist/ycode-darwin-amd64 2>/dev/null || true
```

### dist/ycode-darwin-arm64
Cross-compile for macOS arm64.
Generates: dist/ycode-darwin-arm64
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"
TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

mkdir -p dist
GOOS=darwin GOARCH=arm64 go build -trimpath -tags "${TAG_LIST}" -ldflags "${LDFLAGS}" -o dist/ycode-darwin-arm64 ./cmd/ycode/
codesign -f -s - dist/ycode-darwin-arm64 2>/dev/null || true
```

### dist/ycode-windows-amd64.exe
Cross-compile for Windows amd64.
Generates: dist/ycode-windows-amd64.exe
Effects: write
```bash
set -e
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"
TAG_LIST="sqlite,sqlite_unlock_notify,bindata"
if [ -f internal/runtime/wrap/spawn_embed/ycode-spawn.gz ]; then
  TAG_LIST="${TAG_LIST},embed_spawn"
fi
if [ -f ../coreutils/external/ollama/runner_embed/ycode-runner.gz ]; then
  TAG_LIST="${TAG_LIST},embed_runner"
fi

mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -tags "${TAG_LIST}" -ldflags "${LDFLAGS}" -o dist/ycode-windows-amd64.exe ./cmd/ycode/
```

### cross
Cross-compile for all platforms.
Requires: dist/ycode-linux-amd64, dist/ycode-linux-arm64, dist/ycode-darwin-amd64, dist/ycode-darwin-arm64, dist/ycode-windows-amd64.exe
```bash
echo "cross compile complete"
```

### runner-build
Build Ollama runner from source (requires C++ toolchain).
Effects: write
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
./scripts/build-runner.sh
```

### embed-fetch
Download prebuilt embed blobs (runner+podman+vfkit+gvproxy) from the latest GitHub release.
Effects: write
```bash
./scripts/embed-fetch.sh
```

### runner-build-thin
Build thin runner and compress for embedding.
Effects: write
```bash
export GOROOT="$(bashy go env GOROOT)"
export PATH="$GOROOT/bin:$PATH"
./scripts/build-runner-thin.sh
```

### runner-check
Verify runner binary exists and responds to health check.
Effects: read
```bash
./scripts/check-runner.sh
```

### build-single
Alias for compile, with codesign on macOS.
Requires: compile
```bash
if [ "$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi
echo ""
echo "=== Single binary ready: bin/ycode ==="
ls -lh bin/ycode
```

### deploy
Deploy ycode serve.
Effects: write
```bash
set -e
HOST="${HOST:-localhost}"
if [ "${HOST}" = "localhost" ] || [ "${HOST}" = "127.0.0.1" ]; then
  ./scripts/deploy-local.sh
else
  ./scripts/deploy-remote.sh
fi
```

### deploy-local
Deploy to localhost.
Effects: write
```bash
./scripts/deploy-local.sh
```

### deploy-remote
Deploy to remote host.
Effects: write
```bash
./scripts/deploy-remote.sh
```

### validate
Run Go integration tests against running instance.
Effects: read
```bash
./scripts/validate.sh
```

### validate-ui
Run Playwright browser tests against running instance.
Effects: read
```bash
cd e2e && npx playwright test
```

### validate-all
Run all validation: integration + browser.
Requires: validate, validate-ui
```bash
echo "all validation targets complete"
```

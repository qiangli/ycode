VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)"

# Used by the wildcard expansion in TAG_LIST below — Make's $(,) is
# not portable.
comma := ,

# Build tag layers (see docs/strategy.md#feature-tiers):
#   sqlite + sqlite_unlock_notify  embedded Gitea / SQLite
#   bindata                        Gitea bundled assets
#   embed_runner (auto)            llama.cpp inference runner — added
#                                  automatically when the gz exists.
#                                  `make build` produces it on first run
#                                  via `runner-build-if-missing` (which
#                                  delegates to `scripts/build-runner-thin.sh`).
#                                  On darwin/arm64 no extra toolchain is
#                                  needed (Metal is in-tree); other
#                                  platforms need CMake + a C/C++ compiler.
#                                  If the toolchain is missing the script
#                                  warns and exits 0 — the binary still
#                                  builds, ollama features degrade to
#                                  ErrRunnerNotInstalled at runtime.
#   embed_vfkit (auto)             Apple Virtualization Framework helper
#                                  for podman machine on macOS. Added
#                                  automatically when the gz exists.
#                                  Run `make vfkit-embed` to produce it;
#                                  without it `ycode podman machine
#                                  start` defaults to libkrun and aborts
#                                  with "krunkit: executable file not
#                                  found" on macOS hosts without a
#                                  separately-installed krunkit.
#   embed_podman (auto)            containers/podman client binary (built
#                                  with -tags remote on macOS/Windows,
#                                  native on Linux). Added automatically
#                                  when the gz exists. Produced by
#                                  `make build` via podman-embed-if-missing,
#                                  which calls scripts/embed-podman.sh
#                                  (prefers an upstream system podman,
#                                  falls back to building from
#                                  external/podman/cmd/podman/).
#   embed_gvproxy (auto)           containers/gvisor-tap-vsock user-mode
#                                  network proxy for podman machine
#                                  (macOS + Windows; not needed on
#                                  Linux where podman uses its native
#                                  socket directly). Added automatically
#                                  when the gz exists.
TAG_LIST ?= sqlite,sqlite_unlock_notify,bindata$(if $(wildcard internal/runtime/wrap/spawn_embed/ycode-spawn.gz),$(comma)embed_spawn)$(if $(wildcard internal/inference/runner_embed/ycode-runner.gz),$(comma)embed_runner)$(if $(wildcard internal/container/vfkit_embed/vfkit.gz),$(comma)embed_vfkit)$(if $(wildcard internal/container/podman_embed/podman.gz),$(comma)embed_podman)$(if $(wildcard internal/container/gvproxy_embed/gvproxy.gz),$(comma)embed_gvproxy)
TAGS := -tags "$(TAG_LIST)"
PACKAGES := $(shell go list ./... | grep -v '/priorart/')

# Deploy / validate defaults
HOST ?= localhost
PORT ?= 31415
BASE_URL ?= http://$(HOST):$(PORT)

# Export for scripts (VERSION/COMMIT instead of LDFLAGS to avoid quoting issues)
export VERSION COMMIT PACKAGES HOST PORT BASE_URL TAG_LIST

# macOS Xcode 15+ ld warns "ignoring duplicate libraries: '-lc++', '-lobjc'"
# whenever cgo and a downstream cgo lib both pass them. The duplicate is
# harmless (ld dedupes), so silence it via the standard ld switch.
ifeq ($(shell uname),Darwin)
export CGO_LDFLAGS += -Wl,-no_warn_duplicate_libraries
endif

.PHONY: help init sync priorart-list priorart-sync spawn-embed compile compile-full compile-debug build test test-integration test-container test-oci test-gitserver test-ui test-tui test-tui-e2e test-tui-fuzz test-release-smoke test-all vet tidy clean all chrome-extension cross runner-build runner-build-thin runner-build-if-missing runner-check podman-embed podman-embed-if-missing vfkit-embed vfkit-embed-if-darwin gvproxy-embed gvproxy-embed-if-applicable ensure-embeds _compile-inner build-single collector deploy deploy-local deploy-remote validate validate-ui validate-all eval-agentsmd bench-init eval-contract eval-smoke eval-behavioral eval-e2e eval-init eval-all-evals bench-memory bench-memory-quality bench-memory-competitive bench-memory-latency bench-memory-all

.DEFAULT_GOAL := help

help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

# ─── Source Management ──────────────────────────────────────────────────────

init: ## Initialize submodules, fetch plugins, and prepare embedded assets
	@git submodule init 2>&1 | grep -v 'already registered' || true
	@git submodule update --recursive 2>&1 | grep -v '^From \|^Submodule path\| \* branch' || true
	@./scripts/fetch-perses-plugins.sh
	@./scripts/gzip-embeds.sh
	@./scripts/build-gitea-frontend.sh
	@echo "Generating Gitea bindata..."
	@cd external/gitea && go run build/generate-bindata.go options modules/options/bindata.dat 2>&1
	@cd external/gitea && go run build/generate-bindata.go templates modules/templates/bindata.dat 2>&1
	@cd external/gitea && go run build/generate-bindata.go public modules/public/bindata.dat 2>&1
	@cd external/gitea && go run build/generate-bindata.go modules/migration/schemas modules/migration/bindata.dat 2>&1

sync: ## Pull latest changes for all submodules (skips on conflict)
	@./scripts/sync-submodules.sh

priorart-list: ## List all priorart repos with branch and latest commit
	@./scripts/sync-priorart.sh list

priorart-sync: ## Pull latest changes for all priorart repos
	@./scripts/sync-priorart.sh sync

# ─── Build ──────────────────────────────────────────────────────────────────

ensure-embeds: spawn-embed vfkit-embed-if-darwin runner-build-if-missing podman-embed-if-missing gvproxy-embed-if-applicable

# ycode-spawn is a stdlib-only micro shim inside this repo (cmd/ycode-spawn);
# unlike the other embeds it always builds (no fetch track, no soft-skip)
# and the script is a no-op when the source hasn't changed.
spawn-embed: ## Build + compress the ycode-spawn micro shim for embedding
	@./scripts/embed-spawn.sh

# `compile` runs the embed prereqs first, then re-invokes Make for the
# actual `go build`. The sub-make is required because TAG_LIST's
# $(wildcard ...) probes are expanded ONCE per Make invocation (at
# parse time) — embeds produced during the same invocation would
# otherwise be on disk but missing from TAG_LIST, and the go build
# would link without their embed_* tags. The sub-make re-parses
# TAG_LIST after the .gz files exist.
compile: ensure-embeds ## Compile the ycode binary to bin/ (no checks)
	@$(MAKE) --no-print-directory _compile-inner

_compile-inner:
	go build -trimpath $(TAGS) $(LDFLAGS) -o bin/ycode ./cmd/ycode/
	@echo "Built bin/ycode (tags: $(TAG_LIST))"

verify-features: ## Verify the feature registry structure (paths exist, no malformed entries)
	go test -count=1 ./internal/features/...

compile-full: compile ## Alias for `make compile` — kept for back-compat; TAG_LIST now auto-adds every embed_* tag whose .gz exists, so all embeds land in `make build`
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi

compile-debug: ## Compile with debug symbols (for profiling/debugging)
	go build -trimpath $(TAGS) -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/ycode ./cmd/ycode/
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi

build: ## Build with full quality gate: tidy → fmt → vet → compile → test → verify
	@./scripts/build.sh

test: ## Run unit tests with race detector (-short flag)
	go test -short -race $(TAGS) $(PACKAGES)

test-integration: ## Run Go integration tests (requires running server)
	go test -tags integration -v -count=1 ./internal/integration/...

test-container: ## Run container integration tests (requires podman)
	go test -tags integration -race -count=1 -timeout 180s -v ./internal/container/...

test-release-smoke: ## Fast e2e: ollama pull/run + podman build/pull/run (gates releases)
	@echo "Release smoke — pulls a tiny model and a tiny image to verify both substrates work end-to-end."
	@echo "Skips legs cleanly if podman/ollama prerequisites are missing."
	go test -tags "$(TAG_LIST),release_smoke,embed_runner" -count=1 -timeout 600s -v ./internal/integration/ -run 'TestReleaseSmoke_'

test-oci: ## Run OCI self-build integration test (requires podman)
	go test -tags integration -race -count=1 -timeout 600s -v ./internal/container/... -run TestOCIBuildSelf

test-gitserver: ## Run git server workspace integration tests
	go test -tags "integration,sqlite,sqlite_unlock_notify,bindata" -race -count=1 -timeout 240s -v ./internal/gitserver/...

test-ui: ## Run Playwright browser tests (requires running server + npx)
	@cd e2e && npx playwright test; s=$$?; \
		echo ""; \
		echo "To open the HTML report: cd e2e && npx playwright show-report"; \
		exit $$s

test-tui: ## Run TUI integration tests (direct Update + teatest lifecycle)
	go test -tags integration -count=1 -timeout 60s ./internal/cli/...

test-tui-e2e: compile ## Run TUI E2E tests in a PTY (requires compiled binary)
	go test -tags e2e -count=1 -timeout 120s ./internal/cli/...

test-tui-fuzz: ## Run TUI fuzz tests for 30s each
	go test -run='^$$' -fuzz=FuzzToolDetail -fuzztime=30s ./internal/cli/
	go test -run='^$$' -fuzz=FuzzTUIUpdate -fuzztime=30s ./internal/cli/

test-all: test test-container test-gitserver test-tui test-tui-e2e test-integration test-ui ## Run all tests: unit + container + gitserver + TUI + integration + browser

# ─── Evaluation ────────────────────────────────────────────────────────────

eval-agentsmd: ## Validate AGENTS.md quality (static analysis, no LLM)
	go test -v -run TestAnalyze -race ./internal/eval/agentsmd/...

bench-init: ## Run /init E2E benchmark (HOST=localhost for local, HOST=<remote> for remote)
	go test -tags benchmark -count=1 -timeout 35m -v ./internal/eval/benchmark/...

eval-contract: ## Run contract-tier evals (no LLM, deterministic, fast)
	go test -short -race ./internal/eval/...

eval-smoke: ## Run smoke-tier evals (real LLM, pass@k, requires provider)
	go test -tags eval -count=1 -timeout 5m ./internal/eval/smoke/...

eval-behavioral: ## Run behavioral evals (trajectory analysis, requires provider)
	go test -tags eval_behavioral -count=1 -timeout 30m ./internal/eval/behavioral/...

eval-e2e: ## Run E2E evals (full coding tasks, requires provider)
	go test -tags eval_e2e -count=1 -timeout 45m ./internal/eval/e2e/...

eval-init: compile ## Replay /init via aperio (offline; skips if cassette unrecorded)
	go test -count=1 -timeout 120s ./internal/eval/init/...

eval-all-evals: eval-contract eval-smoke eval-behavioral eval-e2e eval-init ## Run all eval tiers

# ─── Memory Benchmarks ───────────────────────────────────────────────────────

bench-memory: ## Memory retrieval quality benchmarks (no LLM, fast)
	go test -run TestBenchmark -v -count=1 ./internal/runtime/memory/...

bench-memory-quality: ## Comprehensive memory quality (large corpus, context metrics)
	go test -run 'TestBenchmark_Quality|TestContextMetrics' -v -count=1 -timeout 2m ./internal/runtime/memory/...

bench-memory-competitive: ## Competitive benchmark (LoCoMo subset, fusion ablation, latency)
	go test -run TestCompetitive -v -count=1 -timeout 5m ./internal/runtime/memory/...

bench-memory-latency: ## Memory and storage operation latency benchmarks
	go test -bench BenchmarkRecall -benchmem -count=3 ./internal/runtime/memory/...
	go test -bench 'BenchmarkBleve|BenchmarkVector' -benchmem -count=3 ./internal/storage/...

bench-memory-all: bench-memory bench-memory-quality bench-memory-competitive bench-memory-latency ## All memory benchmarks

vet: ## Run static analysis
	go vet $(TAGS) $(PACKAGES)

tidy: ## Run mod tidy, fmt, and vet
	@./scripts/tidy.sh

clean: ## Remove build artifacts
	rm -rf bin/ dist/

install: build ## Install ycode + drop-in shims (ollama, podman, docker) to ~/bin/
	@mkdir -p ~/bin
	@# Unlink before copy so the new binary lands on a fresh inode. On macOS,
	@# overwriting a signed Mach-O in place leaves the kernel's per-vnode
	@# cs_blob cache pointing at the previous signature; the next exec then
	@# fails validation ("load code signature error 2") and the process is
	@# SIGKILLed before main() — surfaced as `zsh: killed ycode ...`.
	@rm -f ~/bin/ycode
	@cp bin/ycode ~/bin/ycode
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - ~/bin/ycode 2>/dev/null || true; fi
	@# The `bash` shim is intentionally excluded: dropping it into ~/bin in
	@# front of /bin/bash hijacks every shell — interactive prompts, system
	@# scripts, and CI runners alike — which has caused real mayhem in past
	@# sessions. If you want bash routed through ycode, opt in explicitly
	@# (e.g. `cp scripts/shims/bash ~/.local/bin/bash` or wire a per-tool
	@# PATH wrapper) rather than blanket-installing it here.
	@# rm -f before cp so an existing symlink (e.g. ~/bin/podman pointing
	@# at /opt/homebrew/bin/podman from a prior brew install) is replaced
	@# rather than followed — cp follows symlinks on write, which would
	@# overwrite the homebrew target and permission-deny on protected paths.
	@for shim in ollama podman docker; do \
		rm -f ~/bin/$$shim; \
		cp scripts/shims/$$shim ~/bin/$$shim && chmod +x ~/bin/$$shim; \
	done
	@echo "Installed ycode + shims (ollama, podman, docker) to ~/bin/"
	@echo 'Make sure ~/bin is in your PATH: export PATH="$$HOME/bin:$$PATH"'

all: build ## Full quality gate (alias for build)

chrome-extension: compile ## Build ycode and print ycode-live Chrome extension setup
	@echo ""
	@echo "ycode binary built. The ycode-live Chrome extension is embedded inside it."
	@echo ""
	@echo "Extract the extension:"
	@echo "  bin/ycode browser setup live"
	@echo "      (or pass --dest <dir> to override the default ~/Downloads/ycode-chrome-ext)"
	@echo ""
	@echo "Load it into Chrome:"
	@echo "  1. Open chrome://extensions"
	@echo "  2. Toggle Developer mode (top-right)"
	@echo "  3. Click 'Load unpacked' → point at the extracted folder"
	@echo "  4. Pin the extension to the toolbar"
	@echo "  5. On the tab you want ycode to drive, click the extension icon → Connect"
	@echo ""
	@echo "Then point ycode at it:"
	@echo "  ycode config set browser.mode live"

# ─── CI Parity ──────────────────────────────────────────────────────────────
#
# `make ci` runs everything GitHub Actions does, in the same container,
# with the same system deps (libbtrfs-dev / libgpgme-dev / libsqlite3-dev
# baked into the image). Use it before push when you've touched anything
# CGO-adjacent or workflow-adjacent — if it passes here, GH won't fail.
#
# Slow (~5–10 min cold; ~2 min after the image is cached). For a fast
# inner loop, `make build` covers fmt/vet/compile/test on the host; the
# Docker pass exists to catch host-environment drift.

DOCKER ?= $(shell command -v docker 2>/dev/null || command -v podman)
CI_IMAGE ?= ycode-builder

ci-image: ## Build the ycode-builder Docker image used by GitHub Actions
	@if [ -z "$(DOCKER)" ]; then echo "neither docker nor podman found in PATH" >&2; exit 1; fi
	$(DOCKER) build -t $(CI_IMAGE) .

ci: ci-image ## Run the full GitHub Actions matrix locally (Docker) — same image, same commands, same deps
	$(DOCKER) run --rm $(CI_IMAGE) make compile
	$(DOCKER) run --rm $(CI_IMAGE) make vet
	$(DOCKER) run --rm $(CI_IMAGE) make test
	$(DOCKER) run --rm $(CI_IMAGE) make verify-features
	$(DOCKER) run --rm $(CI_IMAGE) go test -short -race ./internal/features/...
	$(DOCKER) run --rm $(CI_IMAGE) make test-tui
	$(DOCKER) run --rm $(CI_IMAGE) make test-tui-e2e
	@echo "=== CI parity PASSED ==="

ci-fast: ## Run only the verify-features + unit-test subset (no TUI, no e2e) — assumes ci-image already built
	@if [ -z "$(DOCKER)" ]; then echo "neither docker nor podman found in PATH" >&2; exit 1; fi
	$(DOCKER) run --rm $(CI_IMAGE) make verify-features
	$(DOCKER) run --rm $(CI_IMAGE) go test -short -race ./internal/features/...
	@echo "=== ci-fast PASSED ==="

install-hooks: ## Symlink scripts/git-hooks/* into .git/hooks/ for pre-push CI parity
	@./scripts/install-hooks.sh

# ─── Cross-Compile ──────────────────────────────────────────────────────────

cross: dist/ycode-linux-amd64 dist/ycode-linux-arm64 dist/ycode-darwin-amd64 dist/ycode-darwin-arm64 dist/ycode-windows-amd64.exe ## Cross-compile for all platforms

dist/ycode-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -trimpath $(TAGS) $(LDFLAGS) -o $@ ./cmd/ycode/

dist/ycode-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -trimpath $(TAGS) $(LDFLAGS) -o $@ ./cmd/ycode/

dist/ycode-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build -trimpath $(TAGS) $(LDFLAGS) -o $@ ./cmd/ycode/
	@codesign -f -s - $@ 2>/dev/null || true

dist/ycode-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -trimpath $(TAGS) $(LDFLAGS) -o $@ ./cmd/ycode/
	@codesign -f -s - $@ 2>/dev/null || true

dist/ycode-windows-amd64.exe:
	GOOS=windows GOARCH=amd64 go build -trimpath $(TAGS) $(LDFLAGS) -o $@ ./cmd/ycode/

# ─── Inference Runner ──────────────────────────────────────────────────────

runner-build: ## Build Ollama runner from source (requires C++ toolchain)
	@./scripts/build-runner.sh

# Two-track build dev fast-path. By default the *-if-missing wrappers
# try `embed-fetch` first (downloads prebuilt blobs from the latest
# GitHub release for the current platform — ~30s, no CMake/cgo work)
# and fall through to the source-build script only if the fetch
# didn't produce the .gz. Set BUILD_EMBEDS_FROM_SOURCE=1 to skip the
# fetch entirely and always build from source — what release CI does.
embed-fetch: ## Download prebuilt embed blobs (runner+podman+vfkit+gvproxy) from the latest GitHub release for the current platform
	@./scripts/embed-fetch.sh

runner-build-thin: ## Build thin runner and compress for embedding into ycode
	@./scripts/build-runner-thin.sh

# Idempotent wrapper called from `compile`. Two-track:
#   1. If .gz is already present, no-op.
#   2. Else, unless BUILD_EMBEDS_FROM_SOURCE=1 is set, try fetching
#      a prebuilt blob from the latest GitHub release (~30s).
#   3. If fetch didn't produce the .gz, fall back to the source build
#      (build-runner-thin.sh), which itself skip-cleans when CMake is
#      absent on non-darwin/arm64.
# Release CI sets BUILD_EMBEDS_FROM_SOURCE=1 and skips step 2 entirely.
runner-build-if-missing:
	@if [ ! -f internal/inference/runner_embed/ycode-runner.gz ]; then \
		if [ -z "$$BUILD_EMBEDS_FROM_SOURCE" ]; then \
			./scripts/embed-fetch.sh runner; \
		fi; \
	fi
	@if [ ! -f internal/inference/runner_embed/ycode-runner.gz ]; then \
		./scripts/build-runner-thin.sh; \
	fi

runner-check: ## Verify runner binary exists and responds to health check
	@./scripts/check-runner.sh

podman-embed: ## Compress podman binary for embedding into ycode
	@./scripts/embed-podman.sh

# Internal target: run podman-embed once if the gz is missing.
# Two-track like runner-build-if-missing (fetch first, source build
# second). Source-build via embed-podman.sh prefers a system upstream
# podman, falls back to building from external/podman/cmd/podman/
# (Apache-2.0), and skip-cleans (exit 0) if neither works so
# non-container devs aren't blocked.
podman-embed-if-missing:
	@if [ ! -f internal/container/podman_embed/podman.gz ]; then \
		if [ -z "$$BUILD_EMBEDS_FROM_SOURCE" ]; then \
			./scripts/embed-fetch.sh podman; \
		fi; \
	fi
	@if [ ! -f internal/container/podman_embed/podman.gz ]; then \
		./scripts/embed-podman.sh; \
	fi

vfkit-embed: ## Compress vfkit binary for embedding into ycode (macOS only)
	@./scripts/embed-vfkit.sh

# Internal target: run vfkit-embed once on macOS if the gz is missing.
# Two-track like the others. Other platforms (Linux, Windows) are
# no-ops — the embedded vfkit only helps macOS hosts of
# `ycode podman machine`.
vfkit-embed-if-darwin:
	@if [ "$$(uname)" = "Darwin" ] && [ ! -f internal/container/vfkit_embed/vfkit.gz ]; then \
		if [ -z "$$BUILD_EMBEDS_FROM_SOURCE" ]; then \
			./scripts/embed-fetch.sh vfkit; \
		fi; \
	fi
	@if [ "$$(uname)" = "Darwin" ] && [ ! -f internal/container/vfkit_embed/vfkit.gz ]; then \
		./scripts/embed-vfkit.sh; \
	fi

gvproxy-embed: ## Build gvproxy from module cache and gzip for embedding
	@./scripts/embed-gvproxy.sh

# Internal target: run gvproxy-embed once if the gz is missing AND the
# platform actually needs it. gvproxy is the user-mode network proxy
# `podman machine` forwards through; only relevant where machine
# auto-provisions a VM (macOS + Windows). Linux uses podman's native
# socket directly, so embedding gvproxy there would just bloat the
# binary.
gvproxy-embed-if-applicable:
	@case "$$(uname)" in \
		Darwin|MINGW*|MSYS*|CYGWIN*) \
			if [ ! -f internal/container/gvproxy_embed/gvproxy.gz ]; then \
				if [ -z "$$BUILD_EMBEDS_FROM_SOURCE" ]; then \
					./scripts/embed-fetch.sh gvproxy; \
				fi; \
			fi; \
			if [ ! -f internal/container/gvproxy_embed/gvproxy.gz ]; then \
				./scripts/embed-gvproxy.sh; \
			fi ;; \
	esac

build-single: compile ## Alias for `make compile` — kept for back-compat. The standard `compile` target now produces a single self-contained binary with every embed auto-built (runner via build-runner-thin, podman via embed-podman, vfkit via embed-vfkit on darwin, gvproxy via embed-gvproxy on darwin/windows) when its .gz isn't already present
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi
	@echo ""
	@echo "=== Single binary ready: bin/ycode (tags: $(TAG_LIST)) ==="
	@ls -lh bin/ycode

# ─── Deploy ─────────────────────────────────────────────────────────────────

deploy: ## Deploy ycode serve (HOST=localhost PORT=31415). Use HOST=<remote> for remote deploy
	@if [ "$(HOST)" = "localhost" ] || [ "$(HOST)" = "127.0.0.1" ]; then \
		./scripts/deploy-local.sh; \
	else \
		./scripts/deploy-remote.sh; \
	fi

deploy-local: ## Deploy to localhost
	@./scripts/deploy-local.sh

deploy-remote: ## Deploy to remote host (HOST=<remote> PORT=31415)
	@./scripts/deploy-remote.sh

# ─── Validate ───────────────────────────────────────────────────────────────

validate: ## Run Go integration tests against running instance
	@./scripts/validate.sh

validate-ui: ## Run Playwright browser tests against running instance
	@cd e2e && npx playwright test; s=$$?; \
		echo ""; \
		echo "To open the HTML report: cd e2e && npx playwright show-report"; \
		exit $$s

validate-all: validate validate-ui ## Run all validation: integration + browser

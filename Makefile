VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)"
# sqlite + sqlite_unlock_notify: required for embedded Gitea (in-process git server).
# bindata: embeds Gitea's locale, template, and public asset files into the binary.
TAGS := -tags "sqlite,sqlite_unlock_notify,bindata"
PACKAGES := $(shell go list ./... | grep -v '/priorart/')

# Deploy / validate defaults
HOST ?= localhost
PORT ?= 58080
BASE_URL ?= http://$(HOST):$(PORT)

# Export for scripts (VERSION/COMMIT instead of LDFLAGS to avoid quoting issues)
export VERSION COMMIT PACKAGES HOST PORT BASE_URL

.PHONY: help init sync priorart-list priorart-sync compile compile-full compile-debug build test test-integration test-container test-oci test-gitserver test-ui test-tui test-tui-e2e test-tui-fuzz test-all vet tidy clean all cross runner-download runner-build runner-build-thin runner-check podman-embed vfkit-embed build-single collector deploy deploy-local deploy-remote validate validate-ui validate-all eval-agentsmd bench-init eval-contract eval-smoke eval-behavioral eval-e2e eval-all-evals bench-memory bench-memory-quality bench-memory-competitive bench-memory-latency bench-memory-all

.DEFAULT_GOAL := help

help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

# ─── Source Management ──────────────────────────────────────────────────────

init: ## Initialize submodules, fetch plugins, and prepare embedded assets
	@git submodule init 2>&1 | grep -v 'already registered' || true
	@git submodule update --recursive 2>&1 | grep -v '^From \|^Submodule path\| \* branch' || true
	@./scripts/fetch-perses-plugins.sh
	@./scripts/gzip-embeds.sh
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

compile: ## Compile the ycode binary to bin/ (no checks)
	go build -trimpath $(TAGS) $(LDFLAGS) -o bin/ycode ./cmd/ycode/

compile-full: ## Compile with embedded podman + runner (single binary, all-in-one)
	go build -trimpath -tags "sqlite,sqlite_unlock_notify,bindata,embed_podman,embed_runner" $(LDFLAGS) -o bin/ycode ./cmd/ycode/
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi
	@echo "Built single binary with embedded podman + runner: bin/ycode"

compile-debug: ## Compile with debug symbols (for profiling/debugging)
	go build -trimpath $(TAGS) -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o bin/ycode ./cmd/ycode/
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi

build: ## Build with full quality gate: tidy → fmt → vet → compile → test → verify
	@./scripts/build.sh

test: ## Run unit tests with race detector (-short flag)
	go test -short -race $(PACKAGES)

test-integration: ## Run Go integration tests (requires running server)
	go test -tags integration -v -count=1 ./internal/integration/...

test-container: ## Run container integration tests (requires podman)
	go test -tags integration -race -count=1 -timeout 180s -v ./internal/container/...

test-oci: ## Run OCI self-build integration test (requires podman)
	go test -tags integration -race -count=1 -timeout 600s -v ./internal/container/... -run TestOCIBuildSelf

test-gitserver: ## Run git server workspace integration tests
	go test -tags integration -race -count=1 -timeout 60s -v ./internal/gitserver/...

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

eval-all-evals: eval-contract eval-smoke eval-behavioral eval-e2e ## Run all eval tiers

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
	go vet $(PACKAGES)

tidy: ## Run mod tidy, fmt, and vet
	@./scripts/tidy.sh

clean: ## Remove build artifacts
	rm -rf bin/ dist/

install: build ## Install ycode to ~/bin/
	@mkdir -p ~/bin
	@cp bin/ycode ~/bin/ycode
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - ~/bin/ycode 2>/dev/null || true; fi
	@echo "Installed ycode to ~/bin/ycode"
	@echo 'Make sure ~/bin is in your PATH: export PATH="$$HOME/bin:$$PATH"'

all: build ## Full quality gate (alias for build)

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

runner-download: ## Download pre-built Ollama runner for current platform
	@./scripts/download-runner.sh

runner-build: ## Build Ollama runner from source (requires C++ toolchain)
	@./scripts/build-runner.sh

runner-build-thin: ## Build thin runner and compress for embedding into ycode
	@./scripts/build-runner-thin.sh

runner-check: ## Verify runner binary exists and responds to health check
	@./scripts/check-runner.sh

podman-embed: ## Compress system podman binary for embedding into ycode
	@./scripts/embed-podman.sh

vfkit-embed: ## Compress vfkit binary for embedding into ycode (macOS only)
	@./scripts/embed-vfkit.sh

build-single: podman-embed vfkit-embed runner-build-thin ## Build single self-contained ycode binary
	go build -trimpath -tags "sqlite,sqlite_unlock_notify,bindata,embed_podman,embed_vfkit,embed_runner" $(LDFLAGS) -o bin/ycode ./cmd/ycode/
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi
	@echo ""
	@echo "=== Single binary ready: bin/ycode ==="
	@echo "Includes: embedded podman, embedded vfkit, embedded inference runner"
	@echo "Ship this one file — ycode auto-provisions everything on first run."
	@ls -lh bin/ycode

# ─── Collector ──────────────────────────────────────────────────────────────

collector: ## Build minimal OTEL collector via OCB (requires ocb installed)
	ocb --config configs/otelcol/builder-config.yaml --output-path bin/otelcol

# ─── Deploy ─────────────────────────────────────────────────────────────────

deploy: ## Deploy ycode serve (HOST=localhost PORT=58080). Use HOST=<remote> for remote deploy
	@if [ "$(HOST)" = "localhost" ] || [ "$(HOST)" = "127.0.0.1" ]; then \
		./scripts/deploy-local.sh; \
	else \
		./scripts/deploy-remote.sh; \
	fi

deploy-local: ## Deploy to localhost
	@./scripts/deploy-local.sh

deploy-remote: ## Deploy to remote host (HOST=<remote> PORT=58080)
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

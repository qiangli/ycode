VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"
PACKAGES := $(shell go list ./... | grep -v '/priorart/')

# Deploy defaults
HOST ?= localhost
PORT ?= 58080

# Validate defaults
BASE_URL ?= http://$(HOST):$(PORT)

.PHONY: help init sync compile build test vet tidy clean all cross collector deploy deploy-local deploy-remote validate

.DEFAULT_GOAL := help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

init: ## Initialize and update git submodules recursively
	git submodule init
	git submodule update --recursive

sync: ## Pull latest changes for all submodules
	git submodule foreach --recursive 'git pull origin $$(git rev-parse --abbrev-ref HEAD)'

compile: ## Compile the ycode binary to bin/ (no checks)
	go build $(LDFLAGS) -o bin/ycode ./cmd/ycode/

build: ## Build with full quality gate: tidy → fmt → vet → compile → test → verify
	@echo "=== Step 1: Dependency hygiene ==="
	go mod tidy
	@echo "=== Step 2: Format ==="
	go fmt $(PACKAGES)
	@echo "=== Step 3: Static analysis ==="
	go vet $(PACKAGES)
	@echo "=== Step 4: Build binary ==="
	go build $(LDFLAGS) -o bin/ycode ./cmd/ycode/
	@echo "=== Step 5: Unit tests ==="
	go test -race $(PACKAGES)
	@echo "=== Step 6: Verify ==="
	bin/ycode version
	@echo "=== Build PASSED ==="

test: ## Run all tests with race detector
	go test -race $(PACKAGES)

vet: ## Run static analysis
	go vet $(PACKAGES)

tidy: ## Run mod tidy, fmt, and vet
	go mod tidy
	go fmt $(PACKAGES)
	go vet $(PACKAGES)

clean: ## Remove build artifacts
	rm -rf bin/ dist/

cross: dist/ycode-linux-amd64 dist/ycode-linux-arm64 dist/ycode-darwin-amd64 dist/ycode-darwin-arm64 dist/ycode-windows-amd64.exe ## Cross-compile for all platforms

dist/ycode-linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $@ ./cmd/ycode/

dist/ycode-linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $@ ./cmd/ycode/

dist/ycode-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $@ ./cmd/ycode/

dist/ycode-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $@ ./cmd/ycode/

dist/ycode-windows-amd64.exe:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $@ ./cmd/ycode/

collector: ## Build minimal OTEL collector via OCB (requires ocb installed)
	ocb --config configs/otelcol/builder-config.yaml --output-path bin/otelcol

all: build ## Full quality gate (alias for build)

# ─── Deploy ──────────────────────────────────────────────────────────────────

deploy: ## Deploy ycode serve (HOST=localhost PORT=58080). Use HOST=<remote> for remote deploy
	@if [ "$(HOST)" = "localhost" ] || [ "$(HOST)" = "127.0.0.1" ]; then \
		$(MAKE) deploy-local; \
	else \
		$(MAKE) deploy-remote; \
	fi

deploy-local: ## Deploy to localhost
	@echo "=== Deploy to localhost:$(PORT) ==="
	@test -f bin/ycode || { echo "ERROR: bin/ycode not found — run 'make build' first"; exit 1; }
	@echo "--- Killing existing instances on port $(PORT) ---"
	@lsof -ti :$(PORT) | xargs kill -TERM 2>/dev/null || true
	@sleep 1
	@lsof -ti :$(PORT) | xargs kill -9 2>/dev/null || true
	@if [ -f "$$HOME/.ycode/serve.pid" ]; then \
		kill -TERM $$(cat "$$HOME/.ycode/serve.pid") 2>/dev/null || true; \
		rm -f "$$HOME/.ycode/serve.pid"; \
	fi
	@echo "--- Starting ycode serve ---"
	bin/ycode serve --port $(PORT) --detach
	@sleep 2
	@echo "--- Verifying health ---"
	@curl -sf http://127.0.0.1:$(PORT)/healthz > /dev/null && \
		echo "=== Deploy PASSED — http://localhost:$(PORT)/ ===" || \
		{ echo "=== Deploy FAILED — health check failed ==="; exit 1; }

deploy-remote: ## Deploy to remote host (HOST=<remote> PORT=58080)
	@echo "=== Deploy to $(HOST):$(PORT) ==="
	@echo "--- Checking SSH connectivity ---"
	@ssh -o BatchMode=yes -o ConnectTimeout=5 $(HOST) "echo ok" > /dev/null 2>&1 || { \
		echo "ERROR: Cannot connect to $(HOST) via passwordless SSH."; \
		echo ""; \
		echo "Set up passwordless SSH:"; \
		echo "  1. Generate a key (if needed):  ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519 -N \"\""; \
		echo "  2. Copy key to remote host:     ssh-copy-id $(HOST)"; \
		echo "  3. Verify:                      ssh -o BatchMode=yes $(HOST) \"echo ok\""; \
		echo "  4. Re-run:                      make deploy HOST=$(HOST) PORT=$(PORT)"; \
		exit 1; \
	}
	@test -f bin/ycode || { echo "ERROR: bin/ycode not found — run 'make build' first"; exit 1; }
	@echo "--- Detecting remote architecture ---"
	$(eval REMOTE_OS := $(shell ssh $(HOST) "uname -s" | tr '[:upper:]' '[:lower:]'))
	$(eval REMOTE_ARCH_RAW := $(shell ssh $(HOST) "uname -m"))
	$(eval REMOTE_ARCH := $(shell echo $(REMOTE_ARCH_RAW) | sed 's/x86_64/amd64/;s/aarch64/arm64/'))
	$(eval LOCAL_OS := $(shell uname -s | tr '[:upper:]' '[:lower:]'))
	$(eval LOCAL_ARCH := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/;s/arm64/arm64/'))
	$(eval BINARY := $(if $(filter $(REMOTE_OS)-$(REMOTE_ARCH),$(LOCAL_OS)-$(LOCAL_ARCH)),bin/ycode,dist/ycode-$(REMOTE_OS)-$(REMOTE_ARCH)))
	@if [ "$(REMOTE_OS)-$(REMOTE_ARCH)" != "$(LOCAL_OS)-$(LOCAL_ARCH)" ]; then \
		echo "--- Cross-compiling for $(REMOTE_OS)/$(REMOTE_ARCH) ---"; \
		GOOS=$(REMOTE_OS) GOARCH=$(REMOTE_ARCH) go build $(LDFLAGS) -o $(BINARY) ./cmd/ycode/; \
	fi
	@echo "--- Uploading binary to $(HOST) ---"
	ssh $(HOST) "mkdir -p ~/ycode/bin"
	scp $(BINARY) $(HOST):~/ycode/bin/ycode
	ssh $(HOST) "chmod +x ~/ycode/bin/ycode"
	@echo "--- Killing existing instances on $(HOST):$(PORT) ---"
	@ssh $(HOST) "lsof -ti :$(PORT) | xargs kill -TERM 2>/dev/null; sleep 1; lsof -ti :$(PORT) | xargs kill -9 2>/dev/null; rm -f ~/.ycode/serve.pid; true"
	@echo "--- Starting ycode serve on $(HOST) ---"
	ssh $(HOST) "cd ~/ycode && nohup bin/ycode serve --port $(PORT) > ~/.ycode/serve.log 2>&1 & echo \$$!"
	@sleep 3
	@echo "--- Verifying health ---"
	@ssh $(HOST) "curl -sf http://127.0.0.1:$(PORT)/healthz" > /dev/null && \
		echo "=== Deploy PASSED — http://$(HOST):$(PORT)/ ===" || \
		{ echo "=== Deploy FAILED — health check failed ==="; exit 1; }

# ─── Validate ────────────────────────────────────────────────────────────────

validate: ## Validate running instance (HOST=localhost PORT=58080)
	@echo "=== Validating $(BASE_URL) ==="
	@PASS=0; FAIL=0; SKIP=0; DETAILS=""; \
	IS_LOCAL=false; \
	if [ "$(HOST)" = "localhost" ] || [ "$(HOST)" = "127.0.0.1" ]; then IS_LOCAL=true; fi; \
	\
	run_test() { \
		NAME="$$1"; shift; \
		if eval "$$@" > /dev/null 2>&1; then \
			echo "  [PASS] $$NAME"; \
			PASS=$$((PASS + 1)); \
		else \
			echo "  [FAIL] $$NAME"; \
			FAIL=$$((FAIL + 1)); \
			DETAILS="$$DETAILS\n  - $$NAME"; \
		fi; \
	}; \
	skip_test() { \
		echo "  [SKIP] $$1"; \
		SKIP=$$((SKIP + 1)); \
	}; \
	\
	echo "--- Pre-flight: Connectivity ---"; \
	if ! curl -sf --max-time 5 $(BASE_URL)/healthz > /dev/null 2>&1; then \
		echo "ERROR: No server reachable at $(BASE_URL)"; \
		echo "Run 'make deploy' first."; \
		exit 1; \
	fi; \
	echo "  Server reachable."; \
	echo ""; \
	\
	echo "--- Suite 1: Smoke Tests ---"; \
	run_test "Health endpoint" "curl -sf $(BASE_URL)/healthz"; \
	run_test "Dashboard reachable" "curl -sf -o /dev/null -w '%{http_code}' $(BASE_URL)/dashboard/ | grep -qE '(200|301|302)'"; \
	if [ "$$IS_LOCAL" = "true" ] && [ -f bin/ycode ]; then \
		run_test "Version via CLI" "bin/ycode version"; \
		run_test "Server status" "bin/ycode serve status --port $(PORT)"; \
	else \
		skip_test "Version via CLI (remote)"; \
		skip_test "Server status (remote)"; \
	fi; \
	echo ""; \
	\
	echo "--- Suite 2: Integration Tests ---"; \
	run_test "OTEL Collector traces endpoint" \
		"curl -sf http://$(HOST):4318/v1/traces -X POST -H 'Content-Type: application/json' -d '{\"resourceSpans\":[]}'"; \
	run_test "Prometheus metrics" \
		"curl -sf http://$(HOST):8889/metrics | head -1 | grep -qE '^#'"; \
	run_test "Send test trace" \
		"curl -sf http://$(HOST):4318/v1/traces -X POST -H 'Content-Type: application/json' \
		-d '{\"resourceSpans\":[{\"resource\":{\"attributes\":[{\"key\":\"service.name\",\"value\":{\"stringValue\":\"validate-test\"}}]},\"scopeSpans\":[{\"spans\":[{\"traceId\":\"00000000000000000000000000000001\",\"spanId\":\"0000000000000001\",\"name\":\"validate-smoke\",\"kind\":1,\"startTimeUnixNano\":\"1000000000\",\"endTimeUnixNano\":\"2000000000\"}]}]}]}'"; \
	run_test "Send test metrics" \
		"curl -sf http://$(HOST):4318/v1/metrics -X POST -H 'Content-Type: application/json' \
		-d '{\"resourceMetrics\":[{\"resource\":{\"attributes\":[{\"key\":\"service.name\",\"value\":{\"stringValue\":\"validate-test\"}}]},\"scopeMetrics\":[{\"metrics\":[{\"name\":\"validate_test_counter\",\"sum\":{\"dataPoints\":[{\"asInt\":\"1\",\"startTimeUnixNano\":\"1000000000\",\"timeUnixNano\":\"1000000000\"}],\"isMonotonic\":true,\"aggregationTemporality\":2}}]}]}]}'"; \
	run_test "Send test log" \
		"curl -sf http://$(HOST):4318/v1/logs -X POST -H 'Content-Type: application/json' \
		-d '{\"resourceLogs\":[{\"resource\":{\"attributes\":[{\"key\":\"service.name\",\"value\":{\"stringValue\":\"validate-test\"}}]},\"scopeLogs\":[{\"logRecords\":[{\"timeUnixNano\":\"1000000000\",\"body\":{\"stringValue\":\"validation smoke test\"},\"severityText\":\"INFO\"}]}]}]}'"; \
	run_test "Proxy routing - Jaeger" "curl -sf -o /dev/null -w '%{http_code}' $(BASE_URL)/jaeger/ | grep -qE '(200|301|302)'"; \
	echo ""; \
	\
	echo "--- Suite 3: Acceptance Tests ---"; \
	if [ "$$IS_LOCAL" = "true" ] && [ -f bin/ycode ]; then \
		if [ -n "$$ANTHROPIC_API_KEY" ] || [ -n "$$OPENAI_API_KEY" ]; then \
			run_test "One-shot prompt" "echo 'What is 2+2?' | timeout 30 bin/ycode --no-otel --print 2>/dev/null | grep -q '4'"; \
		else \
			skip_test "One-shot prompt (no API key)"; \
		fi; \
		run_test "Serve status subcommand" "bin/ycode serve status --port $(PORT)"; \
		run_test "Doctor check" "bin/ycode doctor"; \
	else \
		skip_test "One-shot prompt (remote)"; \
		skip_test "Serve status subcommand (remote)"; \
		skip_test "Doctor check (remote)"; \
	fi; \
	echo ""; \
	\
	echo "--- Suite 4: Performance Tests ---"; \
	echo "  Health endpoint latency (50 requests):"; \
	for i in $$(seq 1 50); do \
		curl -sf -o /dev/null -w "%{time_total}\n" $(BASE_URL)/healthz; \
	done | sort -n | awk '{ a[NR]=$$1; s+=$$1 } END { \
		printf "    requests: %d\n", NR; \
		printf "    mean:     %.3fs\n", s/NR; \
		printf "    p50:      %.3fs\n", a[int(NR*0.5)]; \
		printf "    p95:      %.3fs\n", a[int(NR*0.95)]; \
		printf "    p99:      %.3fs\n", a[int(NR*0.99)]; \
	}'; \
	PASS=$$((PASS + 1)); \
	\
	echo "  Trace ingestion throughput (100 batches):"; \
	START=$$(date +%s%N); \
	for i in $$(seq 1 100); do \
		curl -sf http://$(HOST):4318/v1/traces -X POST \
			-H 'Content-Type: application/json' \
			-d '{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"perf-test"}}]},"scopeSpans":[{"spans":[{"traceId":"00000000000000000000000000000002","spanId":"0000000000000099","name":"perf-span","kind":1,"startTimeUnixNano":"1000000000","endTimeUnixNano":"2000000000"}]}]}]}' \
			-o /dev/null & \
	done; \
	wait; \
	END=$$(date +%s%N); \
	ELAPSED=$$(( (END - START) / 1000000 )); \
	echo "    100 batches in $${ELAPSED}ms ($$(( 100000 / (ELAPSED + 1) )) req/s)"; \
	PASS=$$((PASS + 1)); \
	\
	if [ "$$IS_LOCAL" = "true" ] && [ -f bin/ycode ]; then \
		echo "  Binary startup time:"; \
		{ time bin/ycode version > /dev/null 2>&1; } 2>&1 | grep real | sed 's/^/    /'; \
		PASS=$$((PASS + 1)); \
	else \
		skip_test "Binary startup time (remote)"; \
	fi; \
	echo ""; \
	\
	echo "=== Validation Report ==="; \
	echo "Target: $(BASE_URL)"; \
	echo ""; \
	echo "  Passed: $$PASS  Failed: $$FAIL  Skipped: $$SKIP"; \
	echo ""; \
	if [ "$$FAIL" -gt 0 ]; then \
		echo "Failures:$$DETAILS"; \
		echo ""; \
		echo "Validation FAILED"; \
		exit 1; \
	else \
		echo "Validation PASSED"; \
	fi

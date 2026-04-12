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
	go test -short -race $(PACKAGES)
	@echo "=== Step 6: Verify ==="
	bin/ycode version
	@echo "=== Build PASSED ==="

test: ## Run unit tests with race detector (-short flag)
	go test -short -race $(PACKAGES)

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
	@echo "--- Pre-flight: Connectivity ---"
	@curl -sf --max-time 5 $(BASE_URL)/healthz > /dev/null 2>&1 || \
		{ echo "ERROR: No server reachable at $(BASE_URL)"; echo "Run 'make deploy' first."; exit 1; }
	@echo "  Server reachable."
	@echo ""
	HOST=$(HOST) PORT=$(PORT) BASE_URL=$(BASE_URL) \
		go test -tags integration -v -count=1 ./internal/integration/...

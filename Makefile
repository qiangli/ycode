VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"
PACKAGES := $(shell go list ./... | grep -v '/priorart/')

# Deploy / validate defaults
HOST ?= localhost
PORT ?= 58080
BASE_URL ?= http://$(HOST):$(PORT)

# Export for scripts (VERSION/COMMIT instead of LDFLAGS to avoid quoting issues)
export VERSION COMMIT PACKAGES HOST PORT BASE_URL

.PHONY: help init sync compile build test vet tidy clean all cross collector deploy deploy-local deploy-remote validate validate-ui

.DEFAULT_GOAL := help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

# ─── Source Management ──────────────────────────────────────────────────────

init: ## Initialize and update git submodules recursively
	git submodule init && git submodule update --recursive

sync: ## Pull latest changes for all submodules
	git submodule foreach --recursive 'git pull origin $$(git rev-parse --abbrev-ref HEAD)'

# ─── Build ──────────────────────────────────────────────────────────────────

compile: ## Compile the ycode binary to bin/ (no checks)
	go build $(LDFLAGS) -o bin/ycode ./cmd/ycode/

build: ## Build with full quality gate: tidy → fmt → vet → compile → test → verify
	@./scripts/build.sh

test: ## Run unit tests with race detector (-short flag)
	go test -short -race $(PACKAGES)

vet: ## Run static analysis
	go vet $(PACKAGES)

tidy: ## Run mod tidy, fmt, and vet
	@./scripts/tidy.sh

clean: ## Remove build artifacts
	rm -rf bin/ dist/

install: build ## Install ycode to ~/bin/
	@mkdir -p ~/bin
	@cp bin/ycode ~/bin/ycode
	@echo "Installed ycode to ~/bin/ycode"
	@echo 'Make sure ~/bin is in your PATH: export PATH="$$HOME/bin:$$PATH"'

all: build ## Full quality gate (alias for build)

# ─── Cross-Compile ──────────────────────────────────────────────────────────

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

validate: ## Validate running instance (HOST=localhost PORT=58080)
	@./scripts/validate.sh

validate-ui: ## Browser e2e tests (requires running server + npx)
	cd e2e && npx playwright test

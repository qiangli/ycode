VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"
PACKAGES := $(shell go list ./... | grep -v '/priorart/')

.PHONY: help init sync build test vet tidy clean all cross

.DEFAULT_GOAL := help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

init: ## Initialize and update git submodules recursively
	git submodule init
	git submodule update --recursive

sync: ## Pull latest changes for all submodules
	git submodule foreach --recursive 'git pull origin $$(git rev-parse --abbrev-ref HEAD)'

build: ## Build the ycode binary to bin/
	go build $(LDFLAGS) -o bin/ycode ./cmd/ycode/

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

all: vet test build ## Run vet, test, and build

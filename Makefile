VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"

.PHONY: build test vet clean all cross

build:
	go build $(LDFLAGS) -o bin/ycode ./cmd/ycode/

test:
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf bin/ dist/

# Cross-compilation targets
cross: dist/ycode-linux-amd64 dist/ycode-linux-arm64 dist/ycode-darwin-amd64 dist/ycode-darwin-arm64 dist/ycode-windows-amd64.exe

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

all: vet test build

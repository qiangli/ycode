#!/usr/bin/env bash
# Build with full quality gate: tidy → fmt → vet → compile → test → verify.
# Env: VERSION, COMMIT, PACKAGES (set by Makefile)
set -euo pipefail

echo "=== Step 1: Dependency hygiene ==="
go mod tidy

echo "=== Step 2: Format ==="
go fmt ${PACKAGES}

echo "=== Step 3: Static analysis ==="
go vet ${PACKAGES}

echo "=== Step 4: Build binary ==="
go build -trimpath -tags "sqlite,sqlite_unlock_notify,bindata" -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/ycode ./cmd/ycode/
if [ "$(uname)" = "Darwin" ]; then codesign -f -s - bin/ycode 2>/dev/null || true; fi

echo "=== Step 5: Unit tests ==="
go test -short -race -tags "sqlite,sqlite_unlock_notify,bindata" ${PACKAGES}

echo "=== Step 6: Verify ==="
bin/ycode version

echo "=== Build PASSED ==="

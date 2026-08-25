#!/usr/bin/env bash
# The full quality gate, as one command.
#
# Exists so a container (Dockerfile CMD, `dag ci`) can run the gate without
# bashy installed inside the image — the image is a Go toolchain, not an
# AgentOS host. `bashy dag build` is the developer-facing entry point and runs
# the same steps; keep the two in step.
#
# Orchestration only, per the layered rule: sequencing and process management
# here, every assertion in Go.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

packages() {
	local pkgs
	pkgs="$(go list ./... | grep -v '/priorart/')"
	# `go vet $(packages)` with an EMPTY list runs bare in the module root,
	# which has no Go files — "no Go files" then masks whatever emptied the
	# list (this exact failure: VCS-stamp errors made `go list` print nothing,
	# and the gate reported a package-listing bug as a missing-package bug).
	# An empty enumeration is never valid here: fail naming the real symptom.
	if [ -z "$pkgs" ]; then
		echo "gate: go list ./... returned no packages — package enumeration is broken (scroll up for its error); refusing to run a bare go vet" >&2
		return 1
	fi
	printf '%s\n' "$pkgs"
}

echo "==> fmtcheck"
./scripts/fmtcheck.sh

echo "==> vet"
go vet $(packages)

echo "==> verify-features"
go test -count=1 ./internal/features/...

echo "==> compile"
go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev) -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" \
  -o bin/ycode ./cmd/ycode/

echo "==> test"
go test -short -race -count=1 $(packages)

echo "gate: passed"

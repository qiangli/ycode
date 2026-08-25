#!/usr/bin/env bash
# Run mod tidy, fmt, and vet. This is the APPLY step; scripts/fmtcheck.sh is
# the read-only gate (see its header for why the gate must never rewrite).
#
# Package enumeration is computed HERE, not inherited. PACKAGES used to be set
# by the Makefile, which was retired in favour of DAG.md + `bashy dag` — so
# under `set -u` every run died with "PACKAGES: unbound variable" before
# reaching a single go command. Mirrors scripts/gate.sh's packages() helper;
# keep the two in step.
#
# Orchestration only, per the layered rule: sequencing here, assertions in Go.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$root"

packages() {
	local pkgs
	pkgs="$(go list ./... | grep -v '/priorart/')"
	# An empty enumeration is never valid here. `go fmt`/`go vet` with an EMPTY
	# list run bare in the module root, which has no Go files — "no Go files"
	# then masks whatever emptied the list (VCS-stamp errors making `go list`
	# print nothing is the known case). Fail naming the real symptom.
	if [ -z "$pkgs" ]; then
		echo "tidy: go list ./... returned no packages — package enumeration is broken (scroll up for its error); refusing to run a bare go fmt/vet" >&2
		return 1
	fi
	printf '%s\n' "$pkgs"
}

go mod tidy

# Assign first: `set -e` propagates a failing command substitution in a bare
# assignment, but NOT one used as an argument, so `go vet $(packages)` would
# swallow the guard above and run bare anyway.
pkgs="$(packages)"

# shellcheck disable=SC2086  # deliberate word-splitting: one arg per package
go fmt $pkgs
# shellcheck disable=SC2086
go vet $pkgs

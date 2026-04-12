#!/usr/bin/env bash
# Run mod tidy, fmt, and vet.
# Env: PACKAGES (set by Makefile)
set -euo pipefail

go mod tidy
go fmt ${PACKAGES}
go vet ${PACKAGES}

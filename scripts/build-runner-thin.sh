#!/usr/bin/env bash
# Build the thin inference runner (llama.cpp + HTTP endpoints) and compress
# for embedding into the ycode binary.
#
# This builds ONLY the runner subprocess (cmd/runner), not the full Ollama server.
# The result is a ~20MB binary compressed to ~8MB with gzip.
#
# Requirements: Go, CMake (except on darwin/arm64 where Metal is in-tree),
# C/C++ compiler.
# Output: ../coreutils/external/ollama/runner_embed/ycode-runner.gz
#
# Soft-skip policy: this script is invoked as a prereq of `make compile`
# (via runner-build-if-missing). On platforms that need CMake but don't
# have it, `exit 0` with a warning so `make build` still produces a
# working ycode binary — only inference features degrade, surfacing the
# canonical "reinstall ycode" error at runtime which the selfheal
# wrapper classifies as FailureTypeNotInstalled (no infinite restart).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OLLAMA_SRC="${REPO_ROOT}/../coreutils/external/ollama/src"
OUT_DIR="${REPO_ROOT}/../coreutils/external/ollama/runner_embed"
BIN_DIR="${REPO_ROOT}/bin"
BINARY="${BIN_DIR}/ycode-runner"

if [ ! -d "${OLLAMA_SRC}" ]; then
    echo "WARN: Ollama source not found at ${OLLAMA_SRC}" >&2
    echo "      Run 'make init' to initialize submodules, then re-run." >&2
    echo "      Skipping runner build — ollama inference will be disabled at runtime." >&2
    exit 0
fi

# darwin/arm64 uses Apple Metal which is compiled in-tree via Go cgo;
# no CMake needed. Other platforms compile llama.cpp via CMake during
# `go generate` and bail clearly if it's missing.
GOOS="$(uname -s | tr '[:upper:]' '[:lower:]')"
GOARCH="$(uname -m)"
case "${GOARCH}" in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
esac

if [ "${GOOS}/${GOARCH}" != "darwin/arm64" ]; then
    if ! command -v cmake >/dev/null 2>&1; then
        echo "WARN: cmake not found on ${GOOS}/${GOARCH} — skipping runner build." >&2
        echo "      Install cmake + a C/C++ compiler and re-run 'make runner-build-thin'" >&2
        echo "      to enable embedded ollama inference. Build continues without it." >&2
        exit 0
    fi
fi

echo "Building thin inference runner from ${OLLAMA_SRC}/cmd/runner..."
echo "Requirements: Go, CMake, C/C++ compiler"

cd "${OLLAMA_SRC}"

# Ensure go.work doesn't redirect us back into ycode's workspace, where
# coreutils/external/ollama isn't a `use` entry and ./... resolves the wrong way.
# Must be exported BEFORE go generate, not just before go build — the
# previous ordering broke since ycode added its own go.work, which is
# why `make build` no longer produced a working runner.
export GOWORK=off

# Generate the C++ inference engine (llama.cpp compilation).
# Scope to the runner's deps — ./... pulls in app/ui's tscriptify/npm
# directive which we don't need and which isn't in the host toolchain.
echo "Running go generate (compiling llama.cpp — may take several minutes)..."
go generate ./ml/... ./x/...

# Build ONLY the runner binary (not the full ollama server).
echo "Building runner binary..."
mkdir -p "${BIN_DIR}"
CGO_ENABLED=1 go build -trimpath -o "${BINARY}" ./cmd/runner/

# Compress for embedding.
echo "Compressing runner for embedding..."
mkdir -p "${OUT_DIR}"
gzip -9 -c "${BINARY}" > "${OUT_DIR}/ycode-runner.gz"

RAW_SIZE=$(du -h "${BINARY}" | cut -f1)
GZ_SIZE=$(du -h "${OUT_DIR}/ycode-runner.gz" | cut -f1)
echo "Built: ${BINARY} (${RAW_SIZE})"
echo "Compressed: ${OUT_DIR}/ycode-runner.gz (${GZ_SIZE})"
echo ""
echo "To embed in ycode, rebuild with: go build -tags embed_runner ./cmd/ycode/"

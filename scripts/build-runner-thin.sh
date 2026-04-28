#!/usr/bin/env bash
# Build the thin inference runner (llama.cpp + HTTP endpoints) and compress
# for embedding into the ycode binary.
#
# This builds ONLY the runner subprocess (cmd/runner), not the full Ollama server.
# The result is a ~20MB binary compressed to ~8MB with gzip.
#
# Requirements: Go, CMake, C/C++ compiler
# Output: internal/inference/runner_embed/ycode-runner.gz
set -euo pipefail

OLLAMA_SRC="$(cd "$(dirname "$0")/../external/ollama" && pwd)"
OUT_DIR="$(cd "$(dirname "$0")/../internal/inference/runner_embed" && pwd)"
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
BINARY="${BIN_DIR}/ycode-runner"

if [ ! -d "${OLLAMA_SRC}" ]; then
    echo "ERROR: Ollama source not found at ${OLLAMA_SRC}" >&2
    echo "Run 'make init' to initialize submodules." >&2
    exit 1
fi

echo "Building thin inference runner from ${OLLAMA_SRC}/cmd/runner..."
echo "Requirements: Go, CMake, C/C++ compiler"

cd "${OLLAMA_SRC}"

# Generate the C++ inference engine (llama.cpp compilation).
echo "Running go generate (compiling llama.cpp — may take several minutes)..."
go generate ./...

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

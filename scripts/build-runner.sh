#!/usr/bin/env bash
# Build the Ollama runner from source (requires C++ toolchain).
# This compiles the full Ollama binary including the CGo inference engine.
set -euo pipefail

OLLAMA_SRC="$(cd "$(dirname "$0")/../external/ollama" && pwd)"
INSTALL_DIR="${HOME}/.agents/ycode/bin"
BINARY="${INSTALL_DIR}/ollama"

if [ ! -d "${OLLAMA_SRC}" ]; then
    echo "ERROR: Ollama source not found at ${OLLAMA_SRC}" >&2
    echo "Run 'make init' to initialize submodules." >&2
    exit 1
fi

echo "Building Ollama from source at ${OLLAMA_SRC}..."
echo "This requires: Go, CMake, C/C++ compiler"

cd "${OLLAMA_SRC}"

# Generate the C++ inference engine.
echo "Running go generate (this compiles llama.cpp and may take several minutes)..."
go generate ./...

# Build the Ollama binary.
echo "Building Ollama binary..."
go build -o "${BINARY}" .

mkdir -p "${INSTALL_DIR}"
chmod +x "${BINARY}"
echo "Built: ${BINARY}"
"${BINARY}" --version

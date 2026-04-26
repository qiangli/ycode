#!/usr/bin/env bash
# Download the pre-built Ollama binary for the current platform.
# Stores in ~/.agents/ycode/bin/ollama
set -euo pipefail

INSTALL_DIR="${HOME}/.agents/ycode/bin"
BINARY="${INSTALL_DIR}/ollama"

# Detect platform.
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "${ARCH}" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       echo "Unsupported architecture: ${ARCH}" >&2; exit 1 ;;
esac

case "${OS}" in
    darwin) PLATFORM="darwin" ;;
    linux)  PLATFORM="linux" ;;
    *)      echo "Unsupported OS: ${OS}" >&2; exit 1 ;;
esac

# Get the latest release version from GitHub.
echo "Detecting latest Ollama release..."
LATEST=$(curl -sL "https://api.github.com/repos/ollama/ollama/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "${LATEST}" ]; then
    echo "Failed to detect latest Ollama release" >&2
    exit 1
fi
echo "Latest release: ${LATEST}"

# Check if already installed at correct version.
if [ -x "${BINARY}" ]; then
    INSTALLED=$("${BINARY}" --version 2>/dev/null | awk '{print $NF}' || echo "unknown")
    if [ "${INSTALLED}" = "${LATEST#v}" ]; then
        echo "Ollama ${LATEST} already installed at ${BINARY}"
        exit 0
    fi
fi

# Construct download URL.
# Ollama releases use format: ollama-{os}-{arch}
URL="https://github.com/ollama/ollama/releases/download/${LATEST}/ollama-${PLATFORM}-${ARCH}"
if [ "${PLATFORM}" = "darwin" ]; then
    # macOS uses a zip archive.
    URL="https://github.com/ollama/ollama/releases/download/${LATEST}/Ollama-darwin.zip"
fi

mkdir -p "${INSTALL_DIR}"

if [ "${PLATFORM}" = "darwin" ]; then
    echo "Downloading Ollama ${LATEST} for macOS..."
    TMPDIR=$(mktemp -d)
    curl -fSL "${URL}" -o "${TMPDIR}/ollama.zip"
    # Extract just the CLI binary from the archive.
    unzip -o -j "${TMPDIR}/ollama.zip" "*/ollama" -d "${INSTALL_DIR}" 2>/dev/null || {
        # Fallback: try the direct binary URL.
        curl -fSL "https://github.com/ollama/ollama/releases/download/${LATEST}/ollama-darwin" -o "${BINARY}"
    }
    rm -rf "${TMPDIR}"
else
    echo "Downloading Ollama ${LATEST} for ${PLATFORM}/${ARCH}..."
    curl -fSL "${URL}" -o "${BINARY}"
fi

chmod +x "${BINARY}"
echo "Installed: ${BINARY}"
"${BINARY}" --version

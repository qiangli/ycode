#!/usr/bin/env bash
# Locate the vfkit binary and compress it for embedding into ycode.
# vfkit is the Apple Virtualization Framework helper (~5MB).
# Output: internal/container/vfkit_embed/vfkit.gz
set -euo pipefail

OUT_DIR="$(cd "$(dirname "$0")/../internal/container/vfkit_embed" && pwd)"

# Find vfkit binary.
VFKIT=""
if command -v vfkit &>/dev/null; then
    VFKIT="$(command -v vfkit)"
elif [ -f /opt/homebrew/bin/vfkit ]; then
    VFKIT="/opt/homebrew/bin/vfkit"
elif [ -f /usr/local/bin/vfkit ]; then
    VFKIT="/usr/local/bin/vfkit"
elif [ -f /opt/podman/bin/vfkit ]; then
    VFKIT="/opt/podman/bin/vfkit"
fi

if [ -z "$VFKIT" ]; then
    echo "ERROR: vfkit not found. Install it:" >&2
    echo "  brew install vfkit" >&2
    echo "  (or install Podman Desktop which includes vfkit)" >&2
    exit 1
fi

echo "Using vfkit at: ${VFKIT}"
"${VFKIT}" --version 2>/dev/null || true

RAW_SIZE=$(du -h "${VFKIT}" | cut -f1)
echo "Compressing ${VFKIT} (${RAW_SIZE}) for embedding..."

mkdir -p "${OUT_DIR}"
gzip -9 -c "${VFKIT}" > "${OUT_DIR}/vfkit.gz"

GZ_SIZE=$(du -h "${OUT_DIR}/vfkit.gz" | cut -f1)
echo "Compressed: ${OUT_DIR}/vfkit.gz (${GZ_SIZE})"
echo ""
echo "To embed in ycode: go build -tags embed_vfkit ./cmd/ycode/"

#!/usr/bin/env bash
# Download or locate the podman binary and compress it for embedding into ycode.
# Output: internal/container/podman_embed/podman.gz
set -euo pipefail

OUT_DIR="$(cd "$(dirname "$0")/../internal/container/podman_embed" && pwd)"

# Find existing podman binary.
PODMAN=""
if command -v podman &>/dev/null; then
    PODMAN="$(command -v podman)"
elif [ -f /opt/homebrew/bin/podman ]; then
    PODMAN="/opt/homebrew/bin/podman"
elif [ -f /usr/local/bin/podman ]; then
    PODMAN="/usr/local/bin/podman"
fi

if [ -z "$PODMAN" ]; then
    echo "ERROR: podman not found. Install podman first, then re-run." >&2
    echo "  macOS: brew install podman" >&2
    echo "  Linux: apt install podman / dnf install podman" >&2
    exit 1
fi

echo "Using podman at: ${PODMAN}"
"${PODMAN}" --version

RAW_SIZE=$(du -h "${PODMAN}" | cut -f1)
echo "Compressing ${PODMAN} (${RAW_SIZE}) for embedding..."

mkdir -p "${OUT_DIR}"
gzip -9 -c "${PODMAN}" > "${OUT_DIR}/podman.gz"

GZ_SIZE=$(du -h "${OUT_DIR}/podman.gz" | cut -f1)
echo "Compressed: ${OUT_DIR}/podman.gz (${GZ_SIZE})"
echo ""
echo "To embed in ycode: go build -tags embed_podman ./cmd/ycode/"

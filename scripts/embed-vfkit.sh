#!/usr/bin/env bash
# Locate the vfkit binary and compress it for embedding into ycode.
# vfkit is the Apple Virtualization Framework helper (~5MB).
# Output: internal/container/vfkit_embed/vfkit.gz
set -euo pipefail

OUT_DIR="$(cd "$(dirname "$0")/../internal/container/vfkit_embed" && pwd)"

# Locate vfkit. Prefer a system binary (matches the vfkit a user already
# trusts); fall back to building from the Go module cache so a stripped
# install with no homebrew can still produce a single self-contained
# ycode. The fallback matches what embed-gvproxy.sh does for the
# gvisor-tap-vsock cmd main.
VFKIT=""
CLEANUP_VFKIT=""
if command -v vfkit &>/dev/null; then
    VFKIT="$(command -v vfkit)"
elif [ -f /opt/homebrew/bin/vfkit ]; then
    VFKIT="/opt/homebrew/bin/vfkit"
elif [ -f /usr/local/bin/vfkit ]; then
    VFKIT="/usr/local/bin/vfkit"
elif [ -f /opt/podman/bin/vfkit ]; then
    VFKIT="/opt/podman/bin/vfkit"
else
    VFKIT_MOD=$(grep -E '^\s*github.com/crc-org/vfkit\s' external/podman/go.mod | head -1 | awk '{print $1"@"$2}')
    if [ -n "${VFKIT_MOD}" ]; then
        GOMODCACHE=$(go env GOMODCACHE)
        MOD_DIR="${GOMODCACHE}/${VFKIT_MOD}"
        if [ ! -d "${MOD_DIR}/cmd/vfkit" ]; then
            echo "Downloading vfkit module ${VFKIT_MOD}..."
            (cd external/podman && go mod download github.com/crc-org/vfkit)
        fi
        if [ -d "${MOD_DIR}/cmd/vfkit" ]; then
            VFKIT="$(mktemp)"
            CLEANUP_VFKIT="${VFKIT}"
            trap 'rm -f "${CLEANUP_VFKIT}"' EXIT
            echo "Building vfkit from ${MOD_DIR}/cmd/vfkit"
            go build -trimpath -o "${VFKIT}" "${MOD_DIR}/cmd/vfkit"
            if [ "$(uname -s)" = "Darwin" ]; then
                # vfkit calls Virtualization.framework — needs the
                # `com.apple.security.virtualization` entitlement or
                # VZError Code=2 at first VM start. Upstream ships
                # vf.entitlements for exactly this.
                ENT="${MOD_DIR}/vf.entitlements"
                if [ -f "${ENT}" ]; then
                    codesign --force --entitlements "${ENT}" --sign - "${VFKIT}" 2>/dev/null || true
                else
                    codesign --force --sign - "${VFKIT}" 2>/dev/null || true
                fi
            fi
        fi
    fi
fi

if [ -z "$VFKIT" ]; then
    echo "ERROR: vfkit not found and module-cache fallback failed." >&2
    echo "Install with:  brew install vfkit" >&2
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

#!/usr/bin/env bash
# Build gvproxy from the Go module cache and gzip it for embedding into ycode.
# gvproxy (containers/gvisor-tap-vsock) is the user-mode network proxy that
# podman machine forwards host sockets through. Upstream podman ships it as
# a separate package; we embed it so ycode is self-contained.
#
# Output: internal/container/gvproxy_embed/gvproxy.gz
set -euo pipefail

OUT_DIR="$(cd "$(dirname "$0")/../internal/container/gvproxy_embed" && pwd)"
mkdir -p "${OUT_DIR}"

# Resolve the gvisor-tap-vsock module version pinned by go.sum. The cmd/
# main isn't vendored (only pkg/types is) so we go through the module
# cache, which `go mod download` populates without touching the build.
GVPROXY_MOD=$(grep -E '^\s*github.com/containers/gvisor-tap-vsock\s' \
    external/podman/go.mod | head -1 | awk '{print $1"@"$2}')
if [ -z "${GVPROXY_MOD}" ]; then
    echo "ERROR: gvisor-tap-vsock not found in external/podman/go.mod" >&2
    exit 1
fi

GOMODCACHE=$(go env GOMODCACHE)
MOD_DIR="${GOMODCACHE}/${GVPROXY_MOD}"
if [ ! -d "${MOD_DIR}" ]; then
    echo "Module cache miss; downloading ${GVPROXY_MOD}..."
    (cd external/podman && go mod download github.com/containers/gvisor-tap-vsock)
fi
if [ ! -d "${MOD_DIR}/cmd/gvproxy" ]; then
    echo "ERROR: cmd/gvproxy not present in ${MOD_DIR}" >&2
    exit 1
fi

TMP_BIN="$(mktemp)"
trap 'rm -f "${TMP_BIN}"' EXIT
echo "Building gvproxy from ${MOD_DIR}/cmd/gvproxy"
go build -trimpath -o "${TMP_BIN}" "${MOD_DIR}/cmd/gvproxy"

# Ad-hoc codesign on macOS — extracted binaries also re-sign at runtime,
# but signing here means the gzipped bytes match what would land on disk
# if the user already had a signed copy. Avoids a needless re-sign on the
# first machine start.
if [ "$(uname -s)" = "Darwin" ]; then
    codesign --force --sign - "${TMP_BIN}" 2>/dev/null || true
fi

RAW_SIZE=$(du -h "${TMP_BIN}" | cut -f1)
echo "Compressing gvproxy (${RAW_SIZE}) for embedding..."
gzip -9 -c "${TMP_BIN}" > "${OUT_DIR}/gvproxy.gz"

GZ_SIZE=$(du -h "${OUT_DIR}/gvproxy.gz" | cut -f1)
echo "Compressed: ${OUT_DIR}/gvproxy.gz (${GZ_SIZE})"
echo ""
echo "To embed in ycode: go build -tags embed_gvproxy ./cmd/ycode/"

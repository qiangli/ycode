#!/usr/bin/env bash
# Build the ycode-spawn micro shim and compress it for embedding.
# Output: internal/runtime/wrap/spawn_embed/ycode-spawn.gz
#
# Unlike the podman/vfkit/runner embeds there is no fetch track and no
# soft-skip: ycode-spawn is ~150 lines of stdlib-only Go inside this
# repo, builds anywhere the main binary builds, and cross-compiles via
# the same GOOS/GOARCH env the caller already set.
#
# gzip -n + go build -trimpath keep the output byte-stable for an
# unchanged source tree; the cmp guard below avoids touching the .gz
# (and thus invalidating Make's TAG_LIST probe ordering or triggering
# rebuilds) when nothing changed.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${REPO_ROOT}/internal/runtime/wrap/spawn_embed"
OUT_GZ="${OUT_DIR}/ycode-spawn.gz"

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

(cd "${REPO_ROOT}" && go build -trimpath -ldflags="-s -w" -o "${TMP}/ycode-spawn" ./cmd/ycode-spawn/)
GZIP_BIN="/usr/bin/gzip"
[ ! -x "$GZIP_BIN" ] && GZIP_BIN="/bin/gzip"
"$GZIP_BIN" -n -9 -c "${TMP}/ycode-spawn" > "${TMP}/ycode-spawn.gz"

if cmp -s "${TMP}/ycode-spawn.gz" "${OUT_GZ}" 2>/dev/null; then
    exit 0
fi
mv "${TMP}/ycode-spawn.gz" "${OUT_GZ}"
echo "embed-spawn: updated ${OUT_GZ} ($(du -h "${OUT_GZ}" | cut -f1))"

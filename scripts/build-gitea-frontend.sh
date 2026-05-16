#!/usr/bin/env bash
# Build the Gitea web frontend (CSS/JS/fonts via vite) so the subsequent
# `generate-bindata.go public` call has real assets to pack into
# modules/public/bindata.dat. Without this step the embedded UI loads
# but every /assets/* request 404s because public/assets/ contains only
# img/.
#
# Idempotent: skips when public/assets/.vite/manifest.json is newer than
# every input under web_src/ and the vite/tailwind config files. Run
# manually with FORCE=1 to rebuild unconditionally.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GITEA_DIR="${SCRIPT_DIR}/../external/gitea"

if [ ! -d "$GITEA_DIR" ]; then
    echo "build-gitea-frontend: external/gitea missing — run 'git submodule update --init --recursive' first" >&2
    exit 1
fi

MANIFEST="${GITEA_DIR}/public/assets/.vite/manifest.json"

if ! command -v pnpm >/dev/null 2>&1; then
    cat >&2 <<'EOF'
build-gitea-frontend: pnpm not found on PATH.

The embedded Gitea UI needs a one-time frontend build. Install Node 22+
and pnpm 10+, then re-run `make init`. Examples:

  brew install node pnpm                 # macOS Homebrew
  corepack enable && corepack prepare pnpm@latest --activate

Alternatively, skip this step by building with `make compile` directly —
the binary will still run, but pages under /git/ will be missing CSS/JS.
EOF
    exit 1
fi

# Skip when manifest is up to date relative to inputs.
if [ -z "${FORCE:-}" ] && [ -f "$MANIFEST" ]; then
    if [ -z "$(find "${GITEA_DIR}/web_src" \
                    "${GITEA_DIR}/vite.config.ts" \
                    "${GITEA_DIR}/tailwind.config.ts" \
                    "${GITEA_DIR}/pnpm-lock.yaml" \
                    -newer "$MANIFEST" -print -quit 2>/dev/null)" ]; then
        echo "build-gitea-frontend: frontend up to date (set FORCE=1 to rebuild)"
        exit 0
    fi
fi

echo "build-gitea-frontend: installing node deps (this may take a few minutes)..."
(cd "$GITEA_DIR" && pnpm install --frozen-lockfile)

echo "build-gitea-frontend: running vite build..."
(cd "$GITEA_DIR" && pnpm exec vite build)

if [ ! -f "$MANIFEST" ]; then
    echo "build-gitea-frontend: vite finished but $MANIFEST is missing — check vite output above" >&2
    exit 1
fi

echo "build-gitea-frontend: done"

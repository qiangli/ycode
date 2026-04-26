#!/usr/bin/env bash
# Fetch Perses plugin archives from the GitHub release matching the vendored version.
# Downloads to the repo-local embed directory (for go:embed) and the runtime cache.
# Idempotent — skips download if already present.

set -euo pipefail

PERSES_VERSION="0.53.1"

# Repo-local embed directory (for go:embed into the binary).
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
EMBED_DIR="${SCRIPT_DIR}/../internal/observability/plugins/archive"

# Check if archives already exist in embed dir.
if [ -d "$EMBED_DIR" ] && ls "$EMBED_DIR"/*.tar.gz &>/dev/null; then
    exit 0
fi

# Detect platform.
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
esac

TARBALL="perses_${PERSES_VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/perses/perses/releases/download/v${PERSES_VERSION}/${TARBALL}"

echo "Downloading Perses v${PERSES_VERSION} plugins..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -sfL "$URL" -o "$TMPDIR/perses.tar.gz"
tar xzf "$TMPDIR/perses.tar.gz" -C "$TMPDIR" plugins-archive/

mkdir -p "$EMBED_DIR"
cp "$TMPDIR/plugins-archive/"*.tar.gz "$EMBED_DIR/"

echo "Perses plugins installed to $EMBED_DIR ($(ls "$EMBED_DIR"/*.tar.gz | wc -l | tr -d ' ') archives)"

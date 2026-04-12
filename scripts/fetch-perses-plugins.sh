#!/usr/bin/env bash
# Fetch Perses plugin archives from the GitHub release matching the vendored version.
# Plugins are stored in ~/.ycode/observability/perses/plugins-archive/.
# Idempotent — skips download if already present.

set -euo pipefail

PERSES_VERSION="0.53.1"
DEST_DIR="${1:-$HOME/.ycode/observability/perses/plugins-archive}"

if [ -d "$DEST_DIR" ] && [ "$(ls -A "$DEST_DIR" 2>/dev/null)" ]; then
    echo "Perses plugins already present at $DEST_DIR"
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

mkdir -p "$DEST_DIR"
cp "$TMPDIR/plugins-archive/"*.tar.gz "$DEST_DIR/"

echo "Perses plugins installed to $DEST_DIR ($(ls "$DEST_DIR" | wc -l | tr -d ' ') archives)"

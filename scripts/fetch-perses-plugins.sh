#!/usr/bin/env bash
# Fetch Perses plugin archives from the GitHub release matching the vendored version.
# Downloads only the 3 plugins used by ycode's dashboards (TimeSeriesChart, StatChart,
# Prometheus), strips non-runtime files, and repacks as trimmed archives for go:embed.
# Idempotent — skips download if already present.

set -euo pipefail

PERSES_VERSION="0.53.1"

# Only these plugins are used by ycode's default dashboards.
PLUGINS=(
    "TimeSeriesChart-0.12.1"
    "StatChart-0.12.1"
    "Prometheus-0.57.1"
)

# Repo-local embed directory (for go:embed into the binary).
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
EMBED_DIR="${SCRIPT_DIR}/../internal/observability/plugins/archive"

# Check if the needed plugin archives already exist.
all_present=true
for p in "${PLUGINS[@]}"; do
    if [ ! -f "$EMBED_DIR/${p}.tar.gz" ]; then
        all_present=false
        break
    fi
done
if $all_present; then
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

# Remove any existing archives (clean slate).
rm -f "$EMBED_DIR"/*.tar.gz

mkdir -p "$EMBED_DIR"
for p in "${PLUGINS[@]}"; do
    archive="$TMPDIR/plugins-archive/${p}.tar.gz"
    if [ ! -f "$archive" ]; then
        echo "WARNING: plugin archive $p not found in release" >&2
        continue
    fi

    # Extract, keep only runtime-required files, repack.
    extract_tmp="$TMPDIR/extract-$p"
    trimmed_tmp="$TMPDIR/trimmed-$p"
    mkdir -p "$extract_tmp" "$trimmed_tmp"
    tar xzf "$archive" -C "$extract_tmp"

    # Runtime-required: __mf/ (JS bundles), package.json, mf-manifest.json
    cp "$extract_tmp/package.json" "$trimmed_tmp/"
    cp "$extract_tmp/mf-manifest.json" "$trimmed_tmp/"
    cp -r "$extract_tmp/__mf" "$trimmed_tmp/"

    tar czf "$EMBED_DIR/${p}.tar.gz" -C "$trimmed_tmp" .

    rm -rf "$extract_tmp" "$trimmed_tmp"
done

echo "Perses plugins installed to $EMBED_DIR (${#PLUGINS[@]} trimmed archives)"
ls -lh "$EMBED_DIR"/*.tar.gz

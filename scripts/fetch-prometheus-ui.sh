#!/usr/bin/env bash
# Builds the Prometheus mantine-ui from the upstream source in go module cache.
# Output: internal/observability/static/prometheus/
set -euo pipefail

TARGET="internal/observability/static/prometheus"

# Check if UI is already present (skip rebuild).
if [[ -f "$TARGET/index.html" ]] && grep -q 'mantine' "$TARGET/index.html" 2>/dev/null; then
    echo "Prometheus mantine-ui already present at $TARGET"
    exit 0
fi

command -v node >/dev/null 2>&1 || { echo "node is required to build Prometheus UI"; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "npm is required to build Prometheus UI"; exit 1; }

GOMODCACHE=$(go env GOMODCACHE)
PROM_UI_SRC="$GOMODCACHE/github.com/prometheus/prometheus@v0.310.0/web/ui"

if [[ ! -d "$PROM_UI_SRC" ]]; then
    echo "Prometheus module not in go cache. Run: go mod download"
    exit 1
fi

BUILD_DIR=$(mktemp -d)
trap "rm -rf $BUILD_DIR" EXIT

echo "Building Prometheus mantine-ui..."
cp -r "$PROM_UI_SRC"/* "$BUILD_DIR/"
chmod -R u+w "$BUILD_DIR"

cd "$BUILD_DIR"
npm install --prefer-offline 2>&1 | tail -1
npm run build -w "@prometheus-io/lezer-promql" 2>&1 | tail -1
npm run build -w "@prometheus-io/codemirror-promql" 2>&1 | tail -1
npm run build -w "@prometheus-io/mantine-ui" 2>&1 | tail -1
cd - >/dev/null

rm -rf "$TARGET"
mkdir -p "$TARGET"
cp -r "$BUILD_DIR/mantine-ui/dist/"* "$TARGET/"

echo "Prometheus mantine-ui installed at $TARGET ($(du -sh "$TARGET" | cut -f1))"

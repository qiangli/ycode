#!/usr/bin/env bash
# Pre-gzip embedded web UI assets for smaller binary size.
# Replaces original files with .gz versions. The go:embed picks up only .gz files.
# At runtime, a decompressing FS layer serves them transparently.
# Idempotent — skips directories that already contain only .gz files.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="${SCRIPT_DIR}/.."

# Directories containing embeddable web assets.
DIRS=(
    "${ROOT}/internal/observability/static/prometheus"
)

count=0
for dir in "${DIRS[@]}"; do
    if [ ! -d "$dir" ]; then
        continue
    fi
    # Find all files that are not already .gz.
    while IFS= read -r -d '' file; do
        gzip -9 -f "$file"  # -f replaces original with .gz
        count=$((count + 1))
    done < <(find "$dir" -type f ! -name '*.gz' ! -name '.git*' -print0)
done

if [ "$count" -gt 0 ]; then
    echo "Pre-gzipped $count embed files"
fi

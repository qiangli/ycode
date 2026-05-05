#!/usr/bin/env bash
# CI drift gate: confirms the README's auto-generated <!-- BEGIN FEATURES --> /
# <!-- END FEATURES --> block matches what `ycode features readme` would write.
#
# Local fix: run `make readme-features` to regenerate.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="${SCRIPT_DIR}/.."

# Render to a temp copy of README and diff against the real one.
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
cp "${ROOT}/README.md" "$tmp"

# Use go run with the same tags so it loads the embedded registry.
go run -tags "sqlite,sqlite_unlock_notify,bindata" "${ROOT}/cmd/ycode/" features readme --write "$tmp" >/dev/null

if ! diff -u "${ROOT}/README.md" "$tmp"; then
    echo
    echo "README Features section is out of sync with internal/features/registry.yaml" >&2
    echo "Run 'make readme-features' to regenerate." >&2
    exit 1
fi

echo "README features: in sync with registry"

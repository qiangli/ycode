#!/usr/bin/env bash
# Run Go integration tests against a running ycode instance.
# Env: HOST (default: localhost), PORT (default: 58080), BASE_URL (derived)
set -euo pipefail

HOST="${HOST:-localhost}"
PORT="${PORT:-58080}"
BASE_URL="${BASE_URL:-http://${HOST}:${PORT}}"

echo "=== Validating ${BASE_URL} ==="

echo "--- Pre-flight: Connectivity ---"
if ! curl -sf --max-time 5 "${BASE_URL}/healthz" > /dev/null 2>&1; then
    echo "ERROR: No server reachable at ${BASE_URL}"
    echo "Run 'make deploy' first."
    exit 1
fi
echo "  Server reachable."
echo ""

HOST="${HOST}" PORT="${PORT}" BASE_URL="${BASE_URL}" \
    go test -tags integration -v -count=1 ./internal/integration/...

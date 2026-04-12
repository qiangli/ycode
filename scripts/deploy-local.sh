#!/usr/bin/env bash
# Deploy ycode to localhost.
# Env: PORT (default: 58080)
set -euo pipefail

PORT="${PORT:-58080}"

echo "=== Deploy to localhost:${PORT} ==="

if [ ! -f bin/ycode ]; then
    echo "ERROR: bin/ycode not found — run 'make build' first"
    exit 1
fi

echo "--- Killing existing instances on port ${PORT} ---"
lsof -ti :"${PORT}" | xargs kill -TERM 2>/dev/null || true
sleep 1
lsof -ti :"${PORT}" | xargs kill -9 2>/dev/null || true

if [ -f "${HOME}/.ycode/serve.pid" ]; then
    kill -TERM "$(cat "${HOME}/.ycode/serve.pid")" 2>/dev/null || true
    rm -f "${HOME}/.ycode/serve.pid"
fi

echo "--- Starting ycode serve ---"
bin/ycode serve --port "${PORT}" --detach
sleep 2

echo "--- Verifying health ---"
if curl -sf "http://127.0.0.1:${PORT}/healthz" > /dev/null; then
    echo "=== Deploy PASSED — http://localhost:${PORT}/ ==="
else
    echo "=== Deploy FAILED — health check failed ==="
    exit 1
fi

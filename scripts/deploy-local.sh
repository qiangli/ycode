#!/usr/bin/env bash
# Deploy ycode to localhost.
# Env: PORT (default: 31415)
set -euo pipefail

PORT="${PORT:-31415}"

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

echo "--- Verifying health (up to 15s) ---"
for i in $(seq 1 30); do
    if curl -sf "http://127.0.0.1:${PORT}/healthz" > /dev/null 2>&1; then
        echo "=== Deploy PASSED — http://localhost:${PORT}/ (ready after ${i} attempts) ==="
        exit 0
    fi
    sleep 0.5
done
echo "=== Deploy FAILED — /healthz did not respond within 15s ==="
exit 1

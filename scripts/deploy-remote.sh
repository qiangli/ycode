#!/usr/bin/env bash
# Deploy ycode to a remote host via SSH.
# Env: HOST (required), PORT (default: 58080), VERSION, COMMIT (set by Makefile)
set -euo pipefail

HOST="${HOST:?HOST is required}"
PORT="${PORT:-58080}"

echo "=== Deploy to ${HOST}:${PORT} ==="

echo "--- Checking SSH connectivity ---"
if ! ssh -o BatchMode=yes -o ConnectTimeout=5 "${HOST}" "echo ok" > /dev/null 2>&1; then
    echo "ERROR: Cannot connect to ${HOST} via passwordless SSH."
    echo ""
    echo "Set up passwordless SSH:"
    echo "  1. Generate a key (if needed):  ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519 -N \"\""
    echo "  2. Copy key to remote host:     ssh-copy-id ${HOST}"
    echo "  3. Verify:                      ssh -o BatchMode=yes ${HOST} \"echo ok\""
    echo "  4. Re-run:                      make deploy HOST=${HOST} PORT=${PORT}"
    exit 1
fi

if [ ! -f bin/ycode ]; then
    echo "ERROR: bin/ycode not found — run 'make build' first"
    exit 1
fi

echo "--- Detecting remote architecture ---"
REMOTE_OS="$(ssh "${HOST}" "uname -s" | tr '[:upper:]' '[:lower:]')"
REMOTE_ARCH="$(ssh "${HOST}" "uname -m" | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
LOCAL_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
LOCAL_ARCH="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"

if [ "${REMOTE_OS}-${REMOTE_ARCH}" = "${LOCAL_OS}-${LOCAL_ARCH}" ]; then
    BINARY="bin/ycode"
else
    BINARY="dist/ycode-${REMOTE_OS}-${REMOTE_ARCH}"
    echo "--- Cross-compiling for ${REMOTE_OS}/${REMOTE_ARCH} ---"
    GOOS="${REMOTE_OS}" GOARCH="${REMOTE_ARCH}" \
        go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o "${BINARY}" ./cmd/ycode/
fi

echo "--- Uploading binary to ${HOST} ---"
ssh "${HOST}" "mkdir -p ~/ycode/bin"
scp "${BINARY}" "${HOST}:~/ycode/bin/ycode"
ssh "${HOST}" "chmod +x ~/ycode/bin/ycode"

echo "--- Killing existing instances on ${HOST}:${PORT} ---"
ssh "${HOST}" "lsof -ti :${PORT} | xargs kill -TERM 2>/dev/null; sleep 1; lsof -ti :${PORT} | xargs kill -9 2>/dev/null; rm -f ~/.ycode/serve.pid; true"

echo "--- Starting ycode serve on ${HOST} ---"
ssh "${HOST}" "cd ~/ycode && nohup bin/ycode serve --port ${PORT} > ~/.ycode/serve.log 2>&1 & echo \$!"
sleep 3

echo "--- Verifying health ---"
if ssh "${HOST}" "curl -sf http://127.0.0.1:${PORT}/healthz" > /dev/null; then
    echo "=== Deploy PASSED — http://${HOST}:${PORT}/ ==="
else
    echo "=== Deploy FAILED — health check failed ==="
    exit 1
fi

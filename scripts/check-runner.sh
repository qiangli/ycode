#!/usr/bin/env bash
# Verify that the Ollama runner binary exists and is functional.
set -euo pipefail

# Search order matches internal/inference/runner.go discoverRunner().
CANDIDATES=(
    "${OLLAMA_RUNNERS:-}"
    "${HOME}/.agents/ycode/bin/ollama"
    "$(command -v ollama 2>/dev/null || true)"
)

BINARY=""
for candidate in "${CANDIDATES[@]}"; do
    if [ -n "${candidate}" ] && [ -x "${candidate}" ]; then
        BINARY="${candidate}"
        break
    fi
done

if [ -z "${BINARY}" ]; then
    echo "ERROR: Ollama runner not found." >&2
    echo "Install with: make runner-download" >&2
    echo "Or install ollama: https://ollama.com/download" >&2
    exit 1
fi

echo "Found: ${BINARY}"
"${BINARY}" --version

echo "Runner binary OK."

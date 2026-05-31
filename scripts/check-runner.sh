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
    echo "Options:" >&2
    echo "  - Run ycode without the --use-system-binaries flag to use the embedded runner" >&2
    echo "    (requires \`make build\` to have produced internal/inference/runner_embed/ycode-runner.gz)" >&2
    echo "  - Install upstream ollama yourself (https://ollama.com/download), start \`ollama serve\`," >&2
    echo "    then run ycode with --use-system-binaries (or set inference.useSystem: true in settings.json)" >&2
    exit 1
fi

echo "Found: ${BINARY}"
"${BINARY}" --version

echo "Runner binary OK."

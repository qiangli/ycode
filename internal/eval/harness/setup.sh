#!/usr/bin/env bash
# Install eval dependencies on the host.
# These are permissive-licensed Python tools used for advanced evaluation.
#
# Usage: ./setup.sh [--all | --inspect | --swebench | --deepeval | --aider]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

install_inspect() {
    echo "Installing Inspect AI (MIT license)..."
    pip install inspect-ai 2>/dev/null || pip3 install inspect-ai
    echo "  Inspect AI installed."
}

install_swebench() {
    echo "Installing SWE-bench harness (MIT license)..."
    pip install swebench 2>/dev/null || pip3 install swebench
    echo "  SWE-bench installed."
}

install_deepeval() {
    echo "Installing DeepEval (Apache-2.0 license)..."
    pip install deepeval 2>/dev/null || pip3 install deepeval
    echo "  DeepEval installed."
}

install_aider() {
    echo "Installing Aider benchmark (Apache-2.0 license)..."
    pip install aider-chat 2>/dev/null || pip3 install aider-chat
    echo "  Aider installed."
}

install_all() {
    install_inspect
    install_swebench
    install_deepeval
    install_aider
}

case "${1:-all}" in
    --all|all)       install_all ;;
    --inspect)       install_inspect ;;
    --swebench)      install_swebench ;;
    --deepeval)      install_deepeval ;;
    --aider)         install_aider ;;
    *)
        echo "Usage: $0 [--all | --inspect | --swebench | --deepeval | --aider]"
        exit 1
        ;;
esac

echo ""
echo "Eval dependencies installed. Verify with:"
echo "  python -c 'import inspect_ai; print(inspect_ai.__version__)'"
echo "  python -c 'import deepeval; print(deepeval.__version__)'"

#!/usr/bin/env bash
# Sync all priorart/ repos: list or pull latest from origin.
# Usage:
#   sync-priorart.sh list   — list repos with current branch/commit
#   sync-priorart.sh sync   — git pull --ff-only in each repo
set -euo pipefail

PRIORART_DIR="$(cd "$(dirname "$0")/.." && pwd)/priorart"

action="${1:-sync}"

if [ ! -d "$PRIORART_DIR" ]; then
    echo "No priorart/ directory found."
    exit 1
fi

repos=()
for d in "$PRIORART_DIR"/*/; do
    [ -d "$d/.git" ] && repos+=("$d")
done

if [ ${#repos[@]} -eq 0 ]; then
    echo "No git repos found in priorart/"
    exit 0
fi

case "$action" in
    list)
        printf "%-14s %-12s %s\n" "REPO" "BRANCH" "COMMIT"
        printf "%-14s %-12s %s\n" "----" "------" "------"
        for d in "${repos[@]}"; do
            name=$(basename "$d")
            branch=$(git -C "$d" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "detached")
            commit=$(git -C "$d" log -1 --format='%h %s' 2>/dev/null | cut -c1-60)
            printf "%-14s %-12s %s\n" "$name" "$branch" "$commit"
        done
        ;;
    sync)
        failed=()
        for d in "${repos[@]}"; do
            name=$(basename "$d")
            echo "--- Syncing $name ---"
            if git -C "$d" pull --ff-only 2>&1 | sed 's/^/    /'; then
                echo "    OK"
            else
                echo "    FAILED — manual intervention needed in priorart/$name"
                failed+=("$name")
            fi
        done

        echo ""
        if [ ${#failed[@]} -gt 0 ]; then
            echo "=== Sync completed with failures: ${failed[*]} ==="
            exit 1
        fi
        echo "=== All priorart repos synced ==="
        ;;
    *)
        echo "Usage: $0 {list|sync}"
        exit 1
        ;;
esac

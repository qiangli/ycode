#!/usr/bin/env bash
# Sync all submodules with their upstream remotes.
# Uses rebase to preserve local patches. Aborts and reports on conflict.
set -euo pipefail

failed=()

for sub in $(git submodule --quiet foreach --recursive 'echo $sm_path'); do
    echo "--- Syncing $sub ---"
    if git submodule update --remote --rebase -- "$sub" 2>&1; then
        echo "    OK"
    else
        # Abort any in-progress rebase and report.
        (cd "$sub" && git rebase --abort 2>/dev/null || true)
        echo "    CONFLICT — local patches in $sub conflict with upstream"
        echo "    Resolve manually: cd $sub && git rebase origin/main"
        failed+=("$sub")
    fi
done

if [ ${#failed[@]} -gt 0 ]; then
    echo ""
    echo "=== Sync completed with conflicts in: ${failed[*]} ==="
    exit 1
fi

echo ""
echo "=== All submodules synced ==="

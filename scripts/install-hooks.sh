#!/usr/bin/env bash
# install-hooks: symlink scripts/git-hooks/* into .git/hooks/ so they run
# on the matching git event. Idempotent — re-running just refreshes the
# symlinks.
#
# Uninstall: `rm .git/hooks/pre-push` (or whichever hook).
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
src_dir="$repo_root/scripts/git-hooks"
dst_dir="$repo_root/.git/hooks"

if [ ! -d "$src_dir" ]; then
  echo "no hooks to install (missing $src_dir)" >&2
  exit 1
fi

mkdir -p "$dst_dir"
for src in "$src_dir"/*; do
  [ -f "$src" ] || continue
  name=$(basename "$src")
  dst="$dst_dir/$name"
  chmod +x "$src"
  ln -sfn "$src" "$dst"
  echo "installed: $dst -> $src"
done

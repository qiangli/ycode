#!/usr/bin/env bash
# install-hooks: symlink scripts/git-hooks/* into .git/hooks/ so they run
# on the matching git event. Idempotent — re-running just refreshes the
# symlinks.
#
# Uninstall: `rm .git/hooks/pre-push` (or whichever hook).
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
src_dir="$repo_root/scripts/git-hooks"
# NOT "$repo_root/.git/hooks": inside the dhnt umbrella ycode is a submodule, so
# .git is a FILE pointing at ../.git/modules/ycode and that path is not a
# directory to mkdir into. `git rev-parse --git-path` resolves the real gitdir in
# both layouts, so the installer works in a standalone clone and a submodule
# checkout alike — it silently failed in the latter before.
dst_dir=$(cd "$repo_root" && git rev-parse --git-path hooks)
case "$dst_dir" in
  /*) ;;
  *) dst_dir="$repo_root/$dst_dir" ;;
esac

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

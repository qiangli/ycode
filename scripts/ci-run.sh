#!/usr/bin/env bash
# ci-run.sh — compute the podman/docker bind mounts for `bashy dag ci`, then
# run the gate in the container.
#
# WHY THIS EXISTS. The obvious invocation is `-v "$PWD":/src -w /src`. That
# breaks whenever this checkout's git-dir lives OUTSIDE the worktree, which is
# exactly what happens for a submodule checkout inside the dhnt umbrella:
# `.git` there is a FILE, e.g. "gitdir: ../.git/modules/ycode", pointing at
# the umbrella's real git directory. Mounting only $PWD to /src gives the
# container a worktree whose .git file resolves to a path that does not exist
# inside the container, so any git-derived step fails — fmtcheck's
# `git ls-files`, gate.sh's `git describe`/`git rev-parse` for -ldflags — with
#   fatal: not a git repository: /src/../.git/modules/ycode
# Standalone clones never hit this because their git-common-dir (worktree/.git)
# is already nested inside the mounted tree.
#
# THE FIX. Don't remap the worktree to a made-up path at all: mount it, the
# sibling modules named by .sibling-pins, and (only when it lives elsewhere)
# the resolved git-common-dir at their real host absolute paths. Git's relative
# ".git" pointer and go.mod's ../<sibling> replacements then resolve inside the
# container exactly as they do on the host, for both topologies:
#   - standalone clone: git-common-dir is worktree/.git, already covered by
#     the worktree mount — no second mount needed.
#   - umbrella submodule: git-common-dir lives outside the worktree (in the
#     umbrella's .git/modules/<name>) — mount it too, source == target, so
#     the relative lookup still lands on it.
# In both cases the ycode module's replace directives resolve to adjacent sh,
# nadir, and coreutils checkouts. Mounting only ycode and its git-common-dir is
# insufficient: Go then fails while opening ../<sibling>/go.mod.
set -euo pipefail

worktree="$(git rev-parse --path-format=absolute --show-toplevel)"
gitcommondir="$(git rev-parse --path-format=absolute --git-common-dir)"

specs=("$worktree:$worktree")
case "$gitcommondir" in
"$worktree"/*) ;; # nested under the worktree — the mount above already covers it
*) specs+=("$gitcommondir:$gitcommondir") ;;
esac

pins="$worktree/.sibling-pins"
if [ ! -f "$pins" ]; then
	echo "ci-run: missing sibling pin manifest: $pins" >&2
	exit 1
fi
while IFS='=' read -r name _sha; do
	case "$name" in
	''|'#'*) continue ;;
	*[!A-Za-z0-9._-]*)
		echo "ci-run: invalid sibling name in $pins: $name" >&2
		exit 1
		;;
	esac
	sibling="$worktree/../$name"
	if [ ! -d "$sibling" ]; then
		echo "ci-run: missing sibling checkout $sibling; run scripts/bootstrap-siblings.sh" >&2
		exit 1
	fi
	sibling="$(cd "$sibling" && pwd -P)"
	specs+=("$sibling:$sibling")
done < "$pins"

# --print-mounts: emit the computed "src:dst" bind-mount specs, one per line,
# without running anything. Used by the hermetic regression test
# (scripts/ci_mounts_test.go) so it can assert on the plan without a
# container engine.
if [ "${1:-}" = "--print-mounts" ]; then
	printf '%s\n' "${specs[@]}"
	exit 0
fi

mount_args=()
for spec in "${specs[@]}"; do
	mount_args+=(-v "$spec")
done

# Go may normalize go.work.sum during the gate. Preserve its exact pre-run
# contents and restore them on every exit path so verification does not dirty
# the host checkout. An overlapping single-file bind mount is not reliable on
# macOS Podman, and GOWORK=off changes module resolution enough that vet asks
# to rewrite go.mod.
temp_sum="$(mktemp "${TMPDIR:-/tmp}/ycode-go-work-sum.XXXXXX")"
sum_existed=false
if [ -f "$worktree/go.work.sum" ]; then
	cp -p "$worktree/go.work.sum" "$temp_sum"
	sum_existed=true
fi
restore_sum() {
	if $sum_existed; then
		cp -p "$temp_sum" "$worktree/go.work.sum"
	else
		rm -f "$worktree/go.work.sum"
	fi
	rm -f "$temp_sum"
}
trap restore_sum EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

"${DOCKER:-podman}" run --rm "${mount_args[@]}" -w "$worktree" ycode-builder ./scripts/gate.sh

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
# THE FIX. Don't remap the worktree to a made-up path at all: mount it and
# (only when it lives elsewhere) the resolved git-common-dir at their real
# host absolute paths. Git's relative ".git" pointer then resolves inside the
# container exactly as it does on the host, for both topologies:
#   - standalone clone: git-common-dir is worktree/.git, already covered by
#     the worktree mount — no second mount needed.
#   - umbrella submodule: git-common-dir lives outside the worktree (in the
#     umbrella's .git/modules/<name>) — mount it too, source == target, so
#     the relative lookup still lands on it.
# Siblings are go.mod pins: the container builds the pinned modules (no
# workspace is mounted), exactly as CI does, so no sibling checkout is mounted.
set -euo pipefail

worktree="$(git rev-parse --path-format=absolute --show-toplevel)"
gitcommondir="$(git rev-parse --path-format=absolute --git-common-dir)"

specs=("$worktree:$worktree")
case "$gitcommondir" in
"$worktree"/*) ;; # nested under the worktree — the mount above already covers it
*) specs+=("$gitcommondir:$gitcommondir") ;;
esac

# go's VCS stamping does not resolve a submodule's .git FILE: it walks UP from
# the module for the first .git DIRECTORY, so from an umbrella submodule it
# stamps the UMBRELLA's repo — `cd <umbrella>; git status --porcelain` (see
# cmd/go/internal/vcs rootName{".git", isDir: true}). That works on the host,
# where the umbrella is a real repo, and dies in the container with
#   fatal: not a git repository (or any of the parent directories): .git
#   → error obtaining VCS status: exit status 128
# because the ancestor .git was never mounted. Mount it too (standalone
# clones find .git in the worktree itself — already covered — so this adds
# nothing there).
vcswalk="$worktree"
while [ "$vcswalk" != "/" ]; do
	if [ -d "$vcswalk/.git" ]; then
		case "$vcswalk/.git" in
		"$worktree"/*) ;; # inside the mounted worktree — covered
		*) specs+=("$vcswalk/.git:$vcswalk/.git") ;;
		esac
		break
	fi
	vcswalk="$(dirname "$vcswalk")"
done


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

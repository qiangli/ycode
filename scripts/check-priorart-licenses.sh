#!/usr/bin/env bash
# Fail if anything under priorart/ carries a license that could propagate.
#
# priorart/ exists to be READ. Agents are told to study how opencode streams a
# turn, how aider shapes an error, how codex sandboxes a process — and an agent
# that has read GPL source and then writes "its own" version of it has done the
# one thing this project's licensing policy forbids. The safest tree is one where
# the forbidden code is not present to be read.
#
# It is not enough to have deleted it once. These are clones: `git pull` in any of
# them brings it straight back, silently, and nothing would notice. So this runs.
#
# Found and removed 2026-07-14: codex/codex-rs/vendor/bubblewrap (LGPL-2.0) — a
# vendored dep INSIDE codex, not codex itself. codex's own code is Apache-2.0.
#
# Permissive-only is the standing rule: MIT / BSD / Apache-2.0. Nothing whose
# license could propagate (GPL / LGPL / AGPL / MPL / SSPL / BSL / ELv2) may be
# compiled in, embedded, linked, vendored — or, here, left lying around to be
# learned from. If we need a capability that has no permissive implementation, we
# build it from the documentation.
set -euo pipefail

# $0, not BASH_SOURCE: this must run under any POSIX-ish sh, and under `set -u`
# an unset BASH_SOURCE aborts the resolution — at which point `root` is wrong,
# priorart/ is "not found", and the check PASSES on a contaminated tree.
#
# It did exactly that on the first run. A verifier that passes because it could
# not find the thing it was meant to check is not a verifier; it is a green light
# wired to nothing. So the resolved root is now itself verified below.
root="$(cd "$(dirname "$0")/.." && pwd)"

if [ ! -f "$root/go.mod" ]; then
	echo "cannot locate the repo root (resolved: $root) — refusing to report a result" >&2
	echo "A license check that cannot find the tree must FAIL, not pass quietly." >&2
	exit 2
fi

priorart="$root/priorart"

if [ ! -d "$priorart" ]; then
	echo "no priorart/ at $priorart — nothing to check"
	exit 0
fi

# Match on the license TEXT, not on a filename or an SPDX tag: a vendored
# dependency often ships a bare COPYING with no SPDX header anywhere near it.
copyleft='GNU AFFERO|GNU GENERAL PUBLIC LICENSE|GNU LIBRARY GENERAL PUBLIC|GNU LESSER GENERAL PUBLIC|Mozilla Public License|Business Source License|Server Side Public License|Elastic License'

hits=$(grep -rliE "$copyleft" "$priorart" 2>/dev/null |
	grep -iE '/(licen[sc]e|copying)[^/]*$' || true)

if [ -n "$hits" ]; then
	echo "COPYLEFT IN priorart/ — remove these before any agent reads them:" >&2
	echo >&2
	while IFS= read -r f; do
		kind=$(grep -oiE "$copyleft" "$f" | head -1)
		printf '  %-12s %s\n' "$kind" "${f#"$root"/}" >&2
	done <<<"$hits"
	echo >&2
	echo "priorart/ is for LEARNING FROM. An agent that studies copyleft source and" >&2
	echo "then writes 'its own' version of it has done exactly what the policy forbids." >&2
	echo "Delete the offending directory; permissive-only (MIT/BSD/Apache-2.0)." >&2
	exit 1
fi

echo "priorart/: clean (no GPL/LGPL/AGPL/MPL/SSPL/BSL/ELv2)"

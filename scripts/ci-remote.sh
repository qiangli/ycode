#!/usr/bin/env bash
# ci-remote.sh — run the containerized CI matrix on another machine.
#
# WHY THIS EXISTS. `bashy dag ci` builds a Linux image and runs the whole GitHub
# Actions matrix in it. That is the right gate and the wrong thing to run on a
# laptop that is also the dev box: the image build alone needs several GB of
# container storage, and on 2026-08-02 it failed mid-layer with "no space left on
# device" on a host at 99% disk — with the same disk holding live DKS control-plane
# and node-identity volumes. A gate that competes for disk with production state
# is a gate that will be bypassed, and a bypassed gate is worse than none because
# the bypass becomes routine.
#
# So: point CI at a machine with room.
#
#   export YCODE_CI_HOST=builder.example        # ssh target; NOT set by default
#   bashy dag ci                                     # now runs there
#
# Optional:
#   YCODE_CI_DIR    remote checkout dir      (default ~/ci/ycode)
#   YCODE_CI_DOCKER remote container engine  (default "bashy podman")
#   YCODE_CI_TARGET dag target to run       (default "ci")
#
# There is deliberately NO default host. A hostname baked into an OSS repo is
# someone's real machine leaking into a public tree, and it would silently send
# another contributor's build to a box they do not own.
#
# The remote runs the SAME commit you are pushing, fetched from origin — never a
# copy of your working tree. A gate that tests uncommitted local state does not
# tell you whether the pushed commit is green, which is the only question it is
# being asked.
set -euo pipefail

HOST="${YCODE_CI_HOST:-}"
if [ -z "$HOST" ]; then
	cat >&2 <<'EOF'
ci-remote: YCODE_CI_HOST is not set.

  export YCODE_CI_HOST=<ssh-target>

The host needs: ssh access, a container engine, and enough free disk for a
multi-GB image build. `bashy podman` supplies the engine with no system install.
EOF
	exit 2
fi

DIR="${YCODE_CI_DIR:-\$HOME/ci/ycode}"
ENGINE="${YCODE_CI_DOCKER:-bashy podman}"
TARGET="${YCODE_CI_TARGET:-ci}"
SHA="$(git rev-parse HEAD)"
URL="$(git remote get-url origin)"

# THE COMMIT UNDER TEST IS SHIPPED AS A BUNDLE, not fetched from origin.
#
# First version had the remote do `git fetch origin && checkout $SHA`. That cannot
# work from a pre-push hook — the whole point of the gate is to run BEFORE the
# commit reaches origin, so the remote would be told to check out an object no one
# can see yet. It would have "worked" only when re-gating an already-pushed commit,
# i.e. exactly when the gate no longer matters.
#
# Pushing to the remote checkout instead would re-enter this hook (pushing to any
# remote runs pre-push), so it needs --no-verify and the recursion is a trap laid
# for whoever edits it next.
#
# A bundle has neither problem: it is a file, it carries only the objects the
# remote lacks, and it involves no second remote. The base is origin/main when we
# have it, so the delta is the handful of commits being pushed rather than repo
# history.
BASE=""
for candidate in origin/main origin/HEAD; do
	if git rev-parse --verify -q "$candidate" >/dev/null 2>&1; then BASE="$candidate"; break; fi
done
# Two paths, and the empty delta is a NORMAL case rather than an error: re-gating
# an already-pushed commit leaves nothing to ship, and `git bundle` refuses to
# create an empty bundle. When the remote can already reach the commit, let it
# fetch; only bundle what origin has not seen.
BUNDLE=""
if [ -n "$BASE" ] && git merge-base --is-ancestor "$BASE" HEAD 2>/dev/null &&
	[ "$(git rev-list --count "$BASE..HEAD")" -gt 0 ]; then
	BUNDLE="$(mktemp -t ycode-ci-XXXXXX).bundle"
	# shellcheck disable=SC2064  # expand BUNDLE now, not at trap time
	trap "rm -f '$BUNDLE'" EXIT
	git bundle create -q "$BUNDLE" "$BASE..HEAD" HEAD
elif [ -z "$BASE" ]; then
	BUNDLE="$(mktemp -t ycode-ci-XXXXXX).bundle"
	# shellcheck disable=SC2064
	trap "rm -f '$BUNDLE'" EXIT
	git bundle create -q "$BUNDLE" HEAD
fi

echo "ci-remote: $TARGET for ${SHA:0:12} on $HOST" >&2

if [ -n "$BUNDLE" ]; then
	# shellcheck disable=SC2029  # the remote-side expansion is intentional
	ssh -o ConnectTimeout=15 "$HOST" "mkdir -p \$(dirname $DIR) && cat > $DIR.bundle" <"$BUNDLE"
	FETCH="git fetch -q \"\$DIR.bundle\" '+HEAD:refs/ci/candidate' && git checkout -q --detach refs/ci/candidate && rm -f \"\$DIR.bundle\""
else
	FETCH="git fetch -q origin && git checkout -q --detach '$SHA'"
fi

# shellcheck disable=SC2029
ssh -o ConnectTimeout=15 "$HOST" "
set -euo pipefail
export PATH=\$HOME/.local/bin:\$PATH
DIR=$DIR
mkdir -p \$(dirname \"\$DIR\")
if [ ! -d \"\$DIR/.git\" ]; then
  git clone -q '$URL' \"\$DIR\"
fi
cd \"\$DIR\"
$FETCH
echo \"ci-remote: building \$(git rev-parse --short HEAD)\" >&2
bashy dag $TARGET DOCKER='$ENGINE'
"

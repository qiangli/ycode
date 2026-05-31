#!/usr/bin/env bash
# Locate or build the podman binary and compress it for embedding into ycode.
# Output: internal/container/podman_embed/podman.gz
#
# Source priority:
#   1. A system podman the user already has (trusted, version-matched
#      to whatever they were using before; identical to vfkit policy).
#   2. Build podman-remote from external/podman/cmd/podman/ via the Go
#      submodule (Apache 2.0, no external install needed). macOS and
#      Windows use the `remote` build tag (client-only — the actual
#      container engine runs in a podman-machine VM). Linux builds
#      natively (full engine).
#
# Soft-skip policy: if neither source produces a binary, exit 0 with a
# warning so `make build` still produces a working ycode — only
# container features degrade until the embed is present. Symmetric with
# scripts/build-runner-thin.sh.
#
# Never auto-installs upstream podman (no `brew install podman` hint).
# Users who explicitly want the system upstream binary should run ycode
# with --use-system-binaries (or set container.useSystem: true in
# settings.json) and install podman themselves.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${REPO_ROOT}/internal/container/podman_embed"
PODMAN_SRC="${REPO_ROOT}/external/podman"

# Detect target. ycode's Makefile invokes this for the host platform;
# cross-builds would set GOOS/GOARCH explicitly in CI.
GOOS_BUILD="$(uname -s | tr '[:upper:]' '[:lower:]')"
GOARCH_BUILD="$(uname -m)"
case "${GOARCH_BUILD}" in
    x86_64)  GOARCH_BUILD="amd64" ;;
    aarch64) GOARCH_BUILD="arm64" ;;
esac

# 1. System podman — prefer over a fresh build (matches the version
# the user already trusts; avoids unnecessary cgo work). We verify
# `--version` actually responds with upstream-podman output before
# trusting a candidate, because some setups have a "podman" on $PATH
# that is actually ycode's own ycode-podman wrapper (rejects
# --version with cobra-style usage). Falling through to the
# submodule build is the correct response — the wrapper isn't a
# real engine binary we can embed.
PODMAN=""
CLEANUP_PODMAN=""
probe_podman() {
    local candidate="$1"
    [ -x "${candidate}" ] || return 1
    local out
    if ! out="$("${candidate}" --version 2>&1)"; then
        echo "WARN: ${candidate} did not respond to --version (likely a shim, not upstream podman); skipping." >&2
        return 1
    fi
    case "${out}" in
        "podman version "*) return 0 ;;
        *)
            echo "WARN: ${candidate} returned unexpected --version output: ${out%%$'\n'*}" >&2
            echo "      (skipping — looks like a shim rather than upstream podman.)" >&2
            return 1
            ;;
    esac
}

for candidate in \
    "$(command -v podman 2>/dev/null || true)" \
    /opt/homebrew/bin/podman \
    /usr/local/bin/podman \
    /opt/podman/bin/podman; do
    if [ -n "${candidate}" ] && probe_podman "${candidate}"; then
        PODMAN="${candidate}"
        break
    fi
done

# 2. Submodule build fallback — module-cache pattern, mirrors
# embed-vfkit.sh / embed-gvproxy.sh.
if [ -z "${PODMAN}" ]; then
    if [ ! -d "${PODMAN_SRC}/cmd/podman" ]; then
        echo "WARN: podman not found on \$PATH and ${PODMAN_SRC}/cmd/podman is missing." >&2
        echo "      Run 'make init' to initialize submodules, then re-run." >&2
        echo "      Skipping podman embed — container features will be disabled at runtime" >&2
        echo "      unless you run ycode with --use-system-binaries against your own podman." >&2
        exit 0
    fi

    # Build tags per platform:
    #   macOS / Windows → `remote` (client-only; the engine runs in a VM
    #     managed by `podman machine`). Matches upstream's `make podman-remote`.
    #   Linux → native build (full engine, no VM); empty tag list.
    case "${GOOS_BUILD}" in
        darwin|windows)
            BUILDTAGS="remote exclude_graphdriver_btrfs containers_image_openpgp"
            VARIANT="podman-remote (client-only)"
            ;;
        linux)
            BUILDTAGS=""
            VARIANT="podman (native engine)"
            ;;
        *)
            echo "WARN: unsupported GOOS=${GOOS_BUILD} for podman embed; skipping." >&2
            exit 0
            ;;
    esac

    # GOWORK=off so go build resolves against external/podman/go.mod
    # rather than ycode's workspace (which doesn't `use` it).
    PODMAN="$(mktemp)"
    CLEANUP_PODMAN="${PODMAN}"
    trap 'rm -f "${CLEANUP_PODMAN}"' EXIT
    echo "Building ${VARIANT} from ${PODMAN_SRC}/cmd/podman (GOOS=${GOOS_BUILD} GOARCH=${GOARCH_BUILD})..."
    if [ -n "${BUILDTAGS}" ]; then
        (cd "${PODMAN_SRC}" && GOWORK=off go build -trimpath -tags "${BUILDTAGS}" -o "${PODMAN}" ./cmd/podman/)
    else
        (cd "${PODMAN_SRC}" && GOWORK=off go build -trimpath -o "${PODMAN}" ./cmd/podman/)
    fi
    if [ ! -s "${PODMAN}" ]; then
        echo "WARN: podman submodule build produced no output. Skipping embed." >&2
        echo "      Container features will be disabled at runtime unless you run ycode" >&2
        echo "      with --use-system-binaries against your own podman." >&2
        exit 0
    fi
fi

echo "Using podman at: ${PODMAN}"
"${PODMAN}" --version

RAW_SIZE=$(du -h "${PODMAN}" | cut -f1)
echo "Compressing ${PODMAN} (${RAW_SIZE}) for embedding..."

mkdir -p "${OUT_DIR}"
gzip -9 -c "${PODMAN}" > "${OUT_DIR}/podman.gz"

GZ_SIZE=$(du -h "${OUT_DIR}/podman.gz" | cut -f1)
echo "Compressed: ${OUT_DIR}/podman.gz (${GZ_SIZE})"
echo ""
echo "To embed in ycode: go build -tags embed_podman ./cmd/ycode/"

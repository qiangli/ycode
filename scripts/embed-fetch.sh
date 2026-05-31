#!/usr/bin/env bash
# Download prebuilt embed blobs (ycode-runner.gz, podman.gz, vfkit.gz,
# gvproxy.gz) for the current platform from a published GitHub release,
# so local devs don't have to compile llama.cpp + podman from source.
#
# This is the dev fast-path side of the two-track build strategy
# (see ycode/CLAUDE.md "Build & test" + plan
# ~/.claude/plans/obviously-make-build-is-twinkly-melody.md).
# Release CI is the canonical producer (.github/workflows/release.yml)
# — this script is the canonical consumer.
#
# Usage:
#     scripts/embed-fetch.sh [embed...]      # all embeds by default
#     scripts/embed-fetch.sh runner          # just the ollama runner
#     scripts/embed-fetch.sh runner podman   # specific subset
#
# Env overrides:
#     YCODE_RELEASE_TAG=v0.x.y    # default: latest
#     YCODE_RELEASE_REPO=owner/repo  # default: qiangli/ycode
#
# Skip-clean policy: if the release tag has no matching asset for the
# current GOOS/GOARCH, warn + exit 0 (build continues without that
# embed). This is consistent with build-runner-thin.sh and
# embed-podman.sh — non-fatal so non-inference / non-container devs
# aren't blocked.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE_REPO="${YCODE_RELEASE_REPO:-qiangli/ycode}"
RELEASE_TAG="${YCODE_RELEASE_TAG:-latest}"

GOOS_FETCH="$(uname -s | tr '[:upper:]' '[:lower:]')"
GOARCH_FETCH="$(uname -m)"
case "${GOARCH_FETCH}" in
    x86_64)  GOARCH_FETCH="amd64" ;;
    aarch64) GOARCH_FETCH="arm64" ;;
esac
PLATFORM="${GOOS_FETCH}-${GOARCH_FETCH}"

# Embed registry: case-statement instead of an associative array so
# this script works under macOS bash 3.2 (no `declare -A`).
embed_path() {
    case "$1" in
        runner)  echo "internal/inference/runner_embed/ycode-runner.gz" ;;
        podman)  echo "internal/container/podman_embed/podman.gz" ;;
        vfkit)   echo "internal/container/vfkit_embed/vfkit.gz" ;;
        gvproxy) echo "internal/container/gvproxy_embed/gvproxy.gz" ;;
        *)       echo ""; return 1 ;;
    esac
}

# Per-embed asset name in releases. By convention, release assets are
# named "<embed>-<platform>.gz" so a single release uploads all 4×4
# combinations side-by-side. Adjust here if release naming changes.
asset_name() {
    case "$1" in
        runner)  echo "ycode-runner-${PLATFORM}.gz" ;;
        podman)  echo "podman-${PLATFORM}.gz" ;;
        vfkit)   echo "vfkit-${PLATFORM}.gz" ;;
        gvproxy) echo "gvproxy-${PLATFORM}.gz" ;;
        *)       echo ""; return 1 ;;
    esac
}

# Determine which embeds are actually meaningful for this platform.
# Mirrors the per-platform embed bundle in the plan: vfkit is darwin
# only; gvproxy is darwin + windows; runner + podman everywhere.
applicable_for_platform() {
    local embed="$1"
    case "${GOOS_FETCH}/${embed}" in
        */runner|*/podman) return 0 ;;
        darwin/vfkit) return 0 ;;
        darwin/gvproxy|windows/gvproxy) return 0 ;;
        *) return 1 ;;
    esac
}

# Resolve the release tag → API path. `latest` uses the dedicated
# /releases/latest endpoint; explicit tags use /releases/tags/<tag>.
api_path() {
    if [ "${RELEASE_TAG}" = "latest" ]; then
        echo "repos/${RELEASE_REPO}/releases/latest"
    else
        echo "repos/${RELEASE_REPO}/releases/tags/${RELEASE_TAG}"
    fi
}

# Lookup the download URL for an asset name in a given release.
# Uses gh if available (handles auth + redirects) and falls back to a
# curl-only path for environments without gh.
download_url() {
    local asset="$1"
    if command -v gh >/dev/null 2>&1; then
        gh api "$(api_path)" --jq ".assets[] | select(.name == \"${asset}\") | .browser_download_url" 2>/dev/null || true
    else
        curl -fsSL "https://api.github.com/$(api_path)" 2>/dev/null \
            | grep -E '"(name|browser_download_url)"' \
            | awk -F'"' -v want="${asset}" '
                /"name"/      { last_name=$4 }
                /"browser_download_url"/ {
                    if (last_name == want) { print $4; exit }
                }
            ' || true
    fi
}

# Resolve which embeds to fetch.
if [ $# -gt 0 ]; then
    EMBEDS_TO_FETCH=("$@")
else
    EMBEDS_TO_FETCH=(runner podman vfkit gvproxy)
fi

echo "embed-fetch: target ${RELEASE_REPO}@${RELEASE_TAG} for ${PLATFORM}"

for embed in "${EMBEDS_TO_FETCH[@]}"; do
    out_rel="$(embed_path "${embed}" || true)"
    if [ -z "${out_rel}" ]; then
        echo "WARN: unknown embed '${embed}' (expected: runner|podman|vfkit|gvproxy); skipping." >&2
        continue
    fi
    if ! applicable_for_platform "${embed}"; then
        echo "  ${embed}: not applicable on ${PLATFORM}; skipping (no-op)."
        continue
    fi

    out_abs="${REPO_ROOT}/${out_rel}"
    if [ -f "${out_abs}" ]; then
        echo "  ${embed}: ${out_rel} already present; skipping (use \`rm\` to force re-fetch)."
        continue
    fi

    asset="$(asset_name "${embed}")"
    url="$(download_url "${asset}")"
    if [ -z "${url}" ]; then
        echo "WARN: ${embed}: no release asset '${asset}' found at ${RELEASE_REPO}@${RELEASE_TAG} — skipping." >&2
        echo "      Either run \`make ${embed}-embed\` (or runner-build-thin) to build from source," >&2
        echo "      or wait for a release that publishes this asset." >&2
        continue
    fi

    mkdir -p "$(dirname "${out_abs}")"
    tmp="$(mktemp)"
    trap 'rm -f "${tmp}"' EXIT
    echo "  ${embed}: downloading ${asset}..."
    if ! curl -fsSL --retry 3 -o "${tmp}" "${url}"; then
        echo "WARN: ${embed}: download failed; leaving ${out_rel} absent." >&2
        rm -f "${tmp}"
        continue
    fi

    # Validate gzip integrity before promoting — better to fail loud
    # here than have //go:embed pick up a half-downloaded blob.
    if ! gzip -t "${tmp}" 2>/dev/null; then
        echo "WARN: ${embed}: downloaded blob failed gzip -t; not promoting." >&2
        rm -f "${tmp}"
        continue
    fi

    mv -f "${tmp}" "${out_abs}"
    trap - EXIT
    size="$(du -h "${out_abs}" | cut -f1)"
    echo "  ${embed}: ${out_rel} (${size})"
done

echo "embed-fetch: done."

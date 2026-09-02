# ycode builder image
# Mirrors the CI build environment for reproducible Linux builds.
# Usage:
#   podman compose run --rm build        # full quality gate
#   podman compose run --rm compile      # quick compile only
#   podman compose run --rm test         # unit tests only
FROM docker.io/library/golang:1.26-bookworm

# System dependencies: git for toolexec host-exec tier, CGO libs for podman/sqlite.
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    libbtrfs-dev \
    libgpgme-dev \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache dependency downloads in a separate layer.
#
# This used to also COPY pkg/oci/ and pkg/otel/ go.mod files, because
# go.mod once carried `replace` directives pointing at those nested
# modules. Both were removed from ycode in afe1dba ("remove podman/ollama
# completely from ycode — bashy is the home"); pkg/oci now lives in
# coreutils. The COPY lines outlived the directories, so every image build
# died with `COPY pkg/oci/go.mod: no such file or directory` — which in
# turn failed the pre-push hook and blocked pushing ycode ANYWHERE.
# Removing a module means removing what copies it.
# go.mod replaces several modules with sibling paths that
# live OUTSIDE this build context, so `go mod download` cannot see them and
# fails with "reading /coreutils/go.mod: no such file or directory".
#
# CI does not copy them either — .github/workflows/ci.yml runs
# scripts/bootstrap-siblings.sh, which clones each sibling at the SHA pinned
# in .sibling-pins. Do the same here, for the same reason the hook checks
# those pins: the image must build the sibling CI would build, not whatever
# happens to be on this disk.
#
# The script cds to its own parent and clones to $root/../<name>, so from
# WORKDIR /src the siblings land alongside /src — exactly
# where the replace directives point.
COPY go.mod go.sum .sibling-pins ./
COPY scripts/bootstrap-siblings.sh scripts/
COPY external/ external/
# bash, not sh: the script uses `set -o pipefail`, which Debian's /bin/sh
# (dash) rejects outright.
RUN bash scripts/bootstrap-siblings.sh
RUN go mod download

# Copy the rest of the source (invalidates on code changes only).
COPY . .

# Default: full quality gate (same as `bashy dag build`, minus the need for
# bashy inside the image).
CMD ["./scripts/gate.sh"]

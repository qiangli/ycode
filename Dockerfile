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
COPY go.mod go.sum ./
COPY external/ external/
RUN go mod download

# Copy the rest of the source (invalidates on code changes only).
COPY . .

# Default: full quality gate (same as make build).
CMD ["make", "build"]

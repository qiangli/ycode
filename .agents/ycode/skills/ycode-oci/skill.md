---
name: oci
description: Build and run projects in OCI containers using internal embedded podman
user_invocable: true
---

# /oci — Containerized Build & Run

Build and run any project inside an OCI container using ycode's internal embedded podman (no host docker/podman required).

## Usage

```
/oci build .                              # build current project
/oci build /path/to/project               # build from local path
/oci build https://github.com/owner/repo  # clone and build
/oci run <image>                          # run an existing image
/oci                                      # shorthand: build current directory
```

## Arguments

`{{ARGS}}` is parsed as `[subcommand] [target]`:
- **subcommand**: `build` (default) or `run`
- **target**: local path, GitHub URL, or image name

## Instructions

This skill has a builtin Go executor. When invoked, it automatically:

1. Resolves the target (clone if GitHub URL, use local path otherwise)
2. Detects or generates a Dockerfile (checks `Dockerfile`, `Containerfile`, or generates from project type)
3. Builds the OCI image using the internal podman engine
4. Creates a container, runs the build/test command, and reports results

### If the builtin executor is unavailable

Fall back to manual steps:

1. **Resolve target**: If `{{ARGS}}` contains a GitHub URL, clone it to a temp directory. Otherwise use the path directly (default: `.`).

2. **Detect Dockerfile**: Look for `Dockerfile` or `Containerfile` in the project root. If none exists, check for `go.mod` (Go), `package.json` (Node), `Cargo.toml` (Rust), `requirements.txt` (Python) and generate an appropriate Dockerfile.

3. **Build**: Use the container tools to build the image from the project's Dockerfile with the project directory as build context.

4. **Run**: Create a container from the built image, execute the project's default build command, and collect the output.

5. **Report**: Show build success/failure, stdout, stderr, and exit code.

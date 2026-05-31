// Package runner_embed self-extracts an embedded ollama inference
// runner subprocess (llama.cpp + thin HTTP server) into the user cache
// on first use. Built into the ycode binary via `-tags embed_runner`
// when scripts/build-runner-thin.sh has produced
// internal/inference/runner_embed/ycode-runner.gz.
//
// Upstream:    github.com/ollama/ollama (cmd/runner)
// License:     MIT — verified against external/ollama/LICENSE.
//
//	Permissive OSI per the ycode embed policy (see
//	~/.claude/projects/.../feedback_ycode_licenses.md).
//
// How to rebuild the embed:
//
//	make runner-build-thin        # explicit
//	make build                    # implicit, via runner-build-if-missing
//
// Platform notes:
//   - darwin/arm64: Metal compute is in-tree via Go cgo; no CMake required.
//   - linux, darwin/amd64, windows: CMake + a C/C++ compiler required.
//     Without them, build-runner-thin.sh skip-cleans (exit 0) and the
//     resulting ycode binary surfaces ErrRunnerNotInstalled at runtime.
//
// When adding any future embed dependency, verify the upstream LICENSE
// declares a permissive OSS license (MIT / Apache-2.0 / BSD / MPL /
// ISC) and update this doc.go (and the sibling embed packages') with
// the same metadata.
package runner_embed

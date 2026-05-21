//go:build release_smoke && embed_runner

// Why this file exists:
//
// Ollama's scheduler spawns its inference runner by exec'ing
// `os.Executable() runner --model X --port Y` (external/ollama/llm/server.go).
// When this code runs inside a `go test` binary, os.Executable() points at
// the test binary, which has no `runner` subcommand. Without intervention,
// ollama would exec the test binary, Go's flag parser would choke on
// `runner` as an unknown test flag, and inference would never come up.
//
// We catch that exec re-entry in TestMain — if os.Args[1] looks like the
// runner subcommand, we extract the embedded runner and exec into it
// (mirroring cmd/ycode/runner.go), so the test binary plays the same role
// the real ycode binary does in production. Otherwise we let the test
// framework start normally.
//
// This file is gated by both `release_smoke` and `embed_runner` so it
// only compiles into the smoke-test build — other integration tests are
// untouched.

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	runnerEmbed "github.com/qiangli/ycode/internal/inference/runner_embed"
)

func TestMain(m *testing.M) {
	if len(os.Args) >= 2 && os.Args[1] == "runner" {
		execRunnerOrDie(os.Args[2:])
	}
	os.Exit(m.Run())
}

func execRunnerOrDie(args []string) {
	cacheDir := runnerCacheDir()
	binPath, err := runnerEmbed.EnsureRunner(cacheDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ycode runner (test): ensure embedded runner: %v\n", err)
		os.Exit(1)
	}
	argv := append([]string{binPath}, args...)
	if err := syscall.Exec(binPath, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "ycode runner (test): exec %s: %v\n", binPath, err)
		os.Exit(1)
	}
}

// runnerCacheDir mirrors cmd/ycode/runner.go's runnerCacheDir so the
// extracted binary survives across invocations.
func runnerCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "ycode", "bin")
	}
	return filepath.Join(os.TempDir(), "ycode-bin")
}

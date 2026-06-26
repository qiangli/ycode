package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	runnerEmbed "github.com/qiangli/coreutils/external/ollama/runner_embed"
)

// newRunnerCmd adds the `ycode runner` subcommand. It exists so ollama's
// scheduler (server/llm/server.go:StartRunner) — which spawns inference
// runners by calling `os.Executable() runner --model X --port Y` — can
// transparently drive ycode's embedded runner. We extract the gzipped
// ycode-runner binary from runner_embed (or reuse the cached copy) and
// exec-replace into it with the args ollama passed.
//
// Hidden because it's an internal protocol between ollama's scheduler
// and our embedded llama.cpp runner — not something users invoke.
//
// DisableFlagParsing because runner args (`--model`, `--port`, etc.)
// belong to the runner, not to cobra. We pass everything through
// verbatim.
func newRunnerCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "runner [args...]",
		Short:              "Exec-replace into the embedded inference runner (internal — used by ollama's scheduler)",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			binPath, err := runnerEmbed.EnsureRunner(runnerCacheDir())
			if err != nil {
				return fmt.Errorf("ycode runner: ensure embedded runner: %w", err)
			}
			// exec-replace so the runner becomes PID == this process —
			// keeps ollama's process-tracking + signal handling correct.
			argv := append([]string{binPath}, args...)
			if err := syscall.Exec(binPath, argv, os.Environ()); err != nil {
				return fmt.Errorf("exec %s: %w", binPath, err)
			}
			return nil // unreachable on success
		},
	}
}

// runnerCacheDir matches internal/inference/defaultCacheDir so the
// extracted binary survives across invocations.
func runnerCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "ycode", "bin")
	}
	return filepath.Join(os.TempDir(), "ycode-bin")
}

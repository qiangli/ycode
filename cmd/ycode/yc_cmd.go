package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/shell/builtins"
)

// newYcCmd exposes the yc <verb> built-in registry as a top-level
// `ycode yc <verb>` subcommand. The same dispatcher is also wired into
// `ycode shell` as an exec-handler middleware, so users inside ycode
// shell can drop the `ycode` prefix and just type `yc <verb>`.
//
// This subcommand exists because not every agent harness routes its
// shell tool through `ycode shell` (or the ~/bin/ycode-wrappers/ PATH
// shim). For example, Claude Code spawns /bin/zsh by absolute path,
// bypassing PATH lookup entirely. Exposing the verbs as a real cobra
// subcommand makes them reachable from any shell, since the `ycode`
// binary itself is on PATH.
func newYcCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "yc <verb> [args...]",
		Short: "Run a yc <verb> built-in directly (same dispatcher as `ycode shell`)",
		Long: `Run a yc <verb> built-in from any shell, without going through ycode shell.

The yc verb family (symbols, refs, search-symbols, repomap, graph, git,
remember, recall, sandbox, manifest, help) is normally reachable
inside ycode shell as plain shell commands. This subcommand exposes the
same dispatcher to any caller — useful when an agent's bash tool spawns
/bin/zsh or /bin/bash by absolute path and never sees the
~/bin/ycode-wrappers/ shim.

Examples:
  ycode yc help
  ycode yc symbols internal/shell/sentinel.go
  ycode yc refs Classify
  ycode yc repomap --budget=8000`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true, // pass --flags through to the verb verbatim
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			verbName := args[0]
			verb, ok := builtins.Lookup(verbName)
			if !ok {
				return fmt.Errorf("unknown verb %q. Try `ycode yc help`", verbName)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}

			stdio := builtins.Stdio{
				Stdin:  os.Stdin,
				Stdout: os.Stdout,
				Stderr: os.Stderr,
			}

			exit, runErr := verb.Run(context.Background(), args[1:], stdio, cwd)
			if runErr != nil {
				fmt.Fprintf(os.Stderr, "ycode yc %s: %v\n", verbName, runErr)
				if exit == 0 {
					exit = 1
				}
			}
			if exit != 0 {
				os.Exit(exit)
			}
			return nil
		},
	}
	return cmd
}

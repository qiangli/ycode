package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newWeaveCmd is now a deprecation stub. The filesystem-based weave engine
// was re-homed into the AgentOS hub (github.com/qiangli/coreutils/pkg/weave)
// and is driven from the AgentOS shell as `bashy weave`. ycode no longer
// hosts weave; this stub catches any `ycode weave …` invocation (including
// every subverb, via DisableFlagParsing) and points the caller at the new
// home, returning a non-zero exit so stale scripts/skills fail loudly rather
// than silently doing nothing.
//
// Keeping the constructor name lets cmd/ycode/main.go's registration stay
// unchanged. Remove this stub once no caller references `ycode weave`.
func newWeaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "weave",
		Short:              "(moved) weave now runs under the AgentOS shell — use `bashy weave`",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Fprintln(os.Stderr, "ycode weave has moved to the AgentOS shell.")
			fmt.Fprintln(os.Stderr, "Run it as `bashy weave …` instead — same subcommands.")
			fmt.Fprintln(os.Stderr, "(The filesystem weave engine now lives in coreutils/pkg/weave; ycode no longer hosts it.)")
			return fmt.Errorf("ycode weave: removed — use `bashy weave`")
		},
	}
}

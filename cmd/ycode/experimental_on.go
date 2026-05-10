//go:build experimental

package main

import "github.com/spf13/cobra"

// addExperimentalCmds registers commands gated behind the experimental
// build tag. Each phase adds entries here as it lands.
func addExperimentalCmds(root *cobra.Command) {
	root.AddCommand(newBacklogCmd())
	root.AddCommand(newAutopilotCmd())
	root.AddCommand(newForemanCmd())
}

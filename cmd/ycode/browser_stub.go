//go:build !experimental

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newBrowserCmd is a placeholder in the stable build. The MCP-based
// browser backends (playwright, devtools, browsermcp) live behind the
// experimental build tag — see docs/strategy.md#feature-tiers.
func newBrowserCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "browser",
		Short: "Manage browser backends (requires experimental build)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("`ycode browser` requires an experimental build.")
			fmt.Println("Rebuild with: make compile-experimental")
			return nil
		},
	}
}

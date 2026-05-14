//go:build !experimental

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/mcpservers/live"
)

// newBrowserCmd is a thin shell in the stable build. The full
// MCP-based browser backends (playwright, devtools, browsermcp,
// live runtime hub) live behind the `experimental` build tag —
// see docs/strategy.md#feature-tiers — but the ycode-live Chrome
// extension assets ship in every binary so `ycode browser setup
// live` works regardless of build flags. The linker keeps the
// embedded extension FS alive because this command path
// references live.ExtractExtension.
func newBrowserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browser",
		Short: "Browser backends (stable build: setup-only; experimental adds live/probe/solo runtime)",
	}
	cmd.AddCommand(newBrowserSetupStubCmd())
	return cmd
}

func newBrowserSetupStubCmd() *cobra.Command {
	var dest string
	cmd := &cobra.Command{
		Use:   "setup <mode>",
		Short: "Per-mode one-time setup (stable build only knows `live` — extracts the embedded extension)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "live":
				dst := dest
				if dst == "" {
					dst = live.DefaultExtractDir()
				}
				abs, err := live.ExtractExtension(dst)
				if err != nil {
					return err
				}
				fmt.Printf("Extracted ycode-live extension to:\n  %s\n\n", abs)
				fmt.Println("Load it into Chrome:")
				fmt.Println("  1. Open chrome://extensions")
				fmt.Println("  2. Toggle Developer mode (top-right)")
				fmt.Println("  3. Click Load unpacked → point at the path above")
				fmt.Println()
				fmt.Println("Note: the live runtime hub (WebSocket bridge to ycode) requires an experimental build.")
				fmt.Println("Rebuild with: make compile  (experimental is on by default)")
				return nil
			default:
				return fmt.Errorf("`browser setup %s` requires an experimental build; only `setup live` (extension extraction) is available in the stable build", args[0])
			}
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "Override extraction directory (default: ~/Downloads/ycode-chrome-ext)")
	return cmd
}

//go:build experimental

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/live"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/probe"
	"github.com/qiangli/ycode/internal/runtime/mcpservers/solo"
)

func newBrowserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browser",
		Short: "Manage ycode's browser modes (live / probe / solo)",
		Long: `Manage ycode's pure-Go browser stack.

Modes:
  live   ycode-owned Chrome extension; drives the user's real Chrome
  probe  CDP attach to a Chrome started with --remote-debugging-port
  solo   chromedp launches a fresh isolated Chrome

Subcommands:
  setup    One-time setup per mode (e.g. extract the live extension)
  launch   Start Chrome with the right debug flags for probe
  doctor   Diagnose readiness of each mode
  install  Pre-fetch any per-mode prerequisites (currently a no-op)
  login    Open Chrome interactively to complete logins (probe/live)`,
	}
	cmd.AddCommand(
		newBrowserSetupCmd(),
		newBrowserLaunchCmd(),
		newBrowserDoctorCmd(),
		newBrowserInstallCmd(),
		newBrowserLoginCmd(),
	)
	return cmd
}

func defaultProfileDir(mode string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ycode", "browser-profile", mode)
}

func newBrowserSetupCmd() *cobra.Command {
	var dest string
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "setup <mode>",
		Short: "Per-mode one-time setup (live extracts the extension; probe/solo are no-ops)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case mcpservers.ModeLive:
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
				fmt.Println("     (or drag the folder from the file-manager window")
				fmt.Println("      that just opened onto the chrome://extensions tab)")
				fmt.Println("  4. Pin the extension to the toolbar")
				fmt.Println("  5. On the tab you want ycode to drive, click the extension icon → Connect")
				fmt.Println()
				fmt.Printf("Then: `ycode config set browser.mode live` (port default: %d).\n", live.DefaultPort)
				if !noOpen {
					if err := openInFileManager(abs); err == nil {
						fmt.Println()
						fmt.Println("(A file-manager window has been opened at the extracted path.)")
					}
				}
				return nil
			case mcpservers.ModeProbe:
				fmt.Printf("`probe` requires no setup beyond starting Chrome with the debug port — use `ycode browser launch`.\n")
				return nil
			case mcpservers.ModeSolo:
				fmt.Printf("`solo` requires no setup — `ycode browser install solo` is a no-op for now (host Chrome auto-detected).\n")
				return nil
			}
			return fmt.Errorf("unknown mode %q (want: live | probe | solo)", args[0])
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "Extract path (default: ~/Downloads/ycode-chrome-ext)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not pop a file-manager window after extraction")
	return cmd
}

func newBrowserLaunchCmd() *cobra.Command {
	var chromePath string
	var port int
	cmd := &cobra.Command{
		Use:   "launch",
		Short: "Start the user's Chrome with --remote-debugging-port for probe mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := defaultProfileDir(mcpservers.ModeProbe)
			pid, resolved, err := probe.LaunchChrome(chromePath, port, profile)
			if err != nil {
				return err
			}
			fmt.Printf("Launched Chrome %s (pid %d)\n", resolved, pid)
			fmt.Printf("  --remote-debugging-port=%d\n", port)
			fmt.Printf("  --user-data-dir=%s\n", profile)
			fmt.Println()
			fmt.Println("ycode probe mode can now attach. Set `browser.mode=probe` in settings.json.")
			return nil
		},
	}
	cmd.Flags().StringVar(&chromePath, "chrome", "", "Path to Chrome binary (default: auto-detect)")
	cmd.Flags().IntVar(&port, "port", 9222, "Remote debugging port")
	return cmd
}

func newBrowserDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose readiness for each browser mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			fmt.Println("Browser modes:")

			lv := live.New(0)
			fmt.Printf("  live   available=%v   port=%d   ext-dir=%s\n",
				lv.Available(ctx), lv.Port(), live.DefaultExtractDir())

			pr := probe.New("")
			fmt.Printf("  probe  available=%v   target=%s   profile=%s\n",
				pr.Available(ctx), pr.URL(), defaultProfileDir(mcpservers.ModeProbe))

			so := solo.New(solo.Config{})
			c := so.Cfg()
			fmt.Printf("  solo   available=%v   headed=%v   chromePath=%q\n",
				so.Available(ctx), c.Headed, c.ChromePath)

			fmt.Println()
			fmt.Println("Set `browser.mode` in settings.json to activate one of the modes.")
			fmt.Println("Run `ycode browser setup live` once before using live mode.")
			fmt.Println("Run `ycode browser launch` to start Chrome with the debug port for probe mode.")
			return nil
		},
	}
}

func newBrowserInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <mode>",
		Short: "Pre-fetch per-mode prerequisites (currently no-op for all modes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case mcpservers.ModeLive, mcpservers.ModeProbe, mcpservers.ModeSolo:
				fmt.Printf("install %s: no prerequisites to fetch.\n", args[0])
				return nil
			}
			return fmt.Errorf("unknown mode %q", args[0])
		},
	}
}

func newBrowserLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login [mode]",
		Short: "Open Chrome interactively to complete logins (probe or live)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := mcpservers.ModeProbe
			if len(args) == 1 {
				mode = args[0]
			}
			fmt.Printf("`ycode browser login` for %q is not yet implemented (Phase 2 for probe, Phase 5 for live).\n", mode)
			return nil
		},
	}
}

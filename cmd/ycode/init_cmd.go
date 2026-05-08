package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/selfinit"
)

var (
	initRefresh bool
	initDoctor  bool
	initOptOut  bool
	initJSON    bool
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Establish ycode in the current git repo (project + user-scope foreign-tool configs)",
		Long: `Run ycode's self-init pass against the current directory's git repo.

By default ycode runs this automatically on every invocation; the marker
at <repo>/.ycode/.init-done makes idempotent re-runs no-ops. Use this
command to:

  --refresh   Force a regeneration even if the marker matches.
  --doctor    Print what is/would be registered without writing.
  --opt-out   Disable selfinit for this repo (writes <repo>/.ycode/.no-init).

In a fresh repo the first auto-run is enough; this command is mainly
for explicit refreshes after manifest changes or for diagnosing why a
foreign tool isn't seeing a particular ycode capability.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}

			if initOptOut {
				root := selfinit.FindGitRoot(cwd)
				if root == "" {
					return fmt.Errorf("not in a git repo (cwd=%s)", cwd)
				}
				if err := selfinit.WriteOptOut(root); err != nil {
					return fmt.Errorf("write opt-out: %w", err)
				}
				fmt.Printf("ycode selfinit disabled for %s\n", root)
				return nil
			}

			if initDoctor {
				return runInitDoctor(cwd)
			}

			res, err := selfinit.Run(ctx, selfinit.Options{
				Cwd:          cwd,
				YcodeVersion: version,
				Force:        initRefresh,
				Logger:       slog.Default(),
			})
			if err != nil {
				return err
			}
			printInitResult(res)
			return nil
		},
	}
	cmd.Flags().BoolVar(&initRefresh, "refresh", false, "Force regeneration even if marker matches")
	cmd.Flags().BoolVar(&initDoctor, "doctor", false, "Print what would be registered without writing")
	cmd.Flags().BoolVar(&initOptOut, "opt-out", false, "Disable selfinit for this repo")
	cmd.Flags().BoolVar(&initJSON, "json", false, "Print result as JSON")
	return cmd
}

func runInitDoctor(cwd string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := selfinit.FindGitRoot(cwd)
	caps := selfinit.LoadCapabilities(home, selfinit.DefaultPort)

	fmt.Printf("cwd:       %s\n", cwd)
	fmt.Printf("repo root: %s\n", root)
	if root != "" {
		fmt.Printf("opted out: %v\n", selfinit.IsOptedOut(root))
	}
	fmt.Printf("manifest:  %s\n", selfinit.ManifestPath(home))
	fmt.Println()
	fmt.Println("Capabilities (from manifest or baseline):")
	for _, c := range caps {
		switch c.Transport {
		case "stdio":
			fmt.Printf("  %-14s stdio  %s %v\n", c.Name, c.Command, c.Args)
		case "http":
			fmt.Printf("  %-14s http   %s\n", c.Name, c.URL)
		}
	}
	return nil
}

func printInitResult(res selfinit.Result) {
	if initJSON {
		out, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(out))
		return
	}
	if res.OptedOut {
		fmt.Println("ycode selfinit: opted out for this repo")
		return
	}
	if res.Skipped {
		fmt.Println("ycode selfinit: marker matches, no changes needed (use --refresh to force)")
		return
	}
	if res.RepoRoot != "" {
		fmt.Printf("ycode selfinit: established in %s\n", res.RepoRoot)
		for _, f := range res.ProjectFiles {
			fmt.Printf("  ✓ %s\n", f)
		}
	} else {
		fmt.Println("ycode selfinit: not in a git repo; project-scope writes skipped")
	}
	for tool, files := range res.UserFilesByTool {
		fmt.Printf("  ✓ %s (%v)\n", tool, files)
	}
}

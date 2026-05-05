package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/features"
)

// inYcodeSourceTree reports whether the current working directory looks like
// the ycode source repository (has go.mod naming module github.com/qiangli/ycode).
// Returns the repo root and true on match. The on-disk file-existence check in
// `features verify` is only meaningful from inside the source tree; running
// from a user's project would falsely report every internal file as missing.
func inYcodeSourceTree() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		mod := filepath.Join(dir, "go.mod")
		if f, err := os.Open(mod); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(line, "module github.com/qiangli/ycode") {
					f.Close()
					return dir, true
				}
				if strings.HasPrefix(line, "module ") {
					break
				}
			}
			f.Close()
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func newFeaturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "features",
		Short: "List and verify the feature registry (build tiers)",
		Long: "Manages the feature registry that gates which capabilities ship in default\n" +
			"builds vs. behind experimental/wip Go build tags. The registry is the single\n" +
			"source of truth for \"what is ready to ship.\"\n\n" +
			"See docs/strategy.md#feature-tiers for policy and graduation criteria.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all features and their tiers",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := features.Load()
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TIER\tNAME\tFILES\tNOTES")
			for _, t := range []features.Tier{features.TierStable, features.TierExperimental, features.TierWIP} {
				for _, f := range reg.ByTier(t) {
					files := ""
					if len(f.Files) > 0 {
						files = f.Files[0]
						if len(f.Files) > 1 {
							files = fmt.Sprintf("%s (+%d)", files, len(f.Files)-1)
						}
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Tier, f.Name, files, f.Notes)
				}
			}
			return tw.Flush()
		},
	})

	var readmeWrite string
	readmeCmd := &cobra.Command{
		Use:   "readme",
		Short: "Print the stable features as a markdown bullet list (or write into a file via --write)",
		Long: "Renders the stable-tier features from the registry as the bullet list embedded between\n" +
			"<!-- BEGIN FEATURES --> and <!-- END FEATURES --> sentinels in README.md.\n\n" +
			"With no flags, prints the rendered list to stdout.\n" +
			"With --write README.md, replaces the section in-place (idempotent — exits 0\n" +
			"with no change if the file is already in sync).",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := features.Load()
			if err != nil {
				return err
			}
			rendered := features.RenderReadmeFeatures(reg)
			if readmeWrite == "" {
				fmt.Print(rendered)
				return nil
			}
			changed, err := features.ReplaceSection(readmeWrite, rendered)
			if err != nil {
				return err
			}
			if changed {
				fmt.Printf("updated: %s\n", readmeWrite)
			} else {
				fmt.Printf("up to date: %s\n", readmeWrite)
			}
			return nil
		},
	}
	readmeCmd.Flags().StringVar(&readmeWrite, "write", "", "Path to a file containing BEGIN/END FEATURES markers; replaces the section in-place")
	cmd.AddCommand(readmeCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "verify",
		Short: "Verify the registry structure (and codebase paths if run from the ycode source tree)",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := features.Load()
			if err != nil {
				return err
			}
			// Always validate registry structure (Load already calls Validate).
			// On-disk file check only when we're inside the ycode source tree;
			// running from any other repo would false-positive every entry.
			root, inSource := inYcodeSourceTree()
			if !inSource {
				fmt.Printf("registry: structurally valid (%d features)\n", len(reg.Features))
				fmt.Println("note: on-disk file check skipped — not running inside the ycode source tree")
				return nil
			}
			issues := features.Verify(reg, root)
			for _, iss := range issues {
				fmt.Fprintln(os.Stderr, iss)
			}
			if len(issues) > 0 {
				return fmt.Errorf("%d feature registry verification issue(s)", len(issues))
			}
			fmt.Printf("registry: ok (%d features, all declared paths exist)\n", len(reg.Features))
			return nil
		},
	})

	return cmd
}

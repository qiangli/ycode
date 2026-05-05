package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/features"
)

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

	cmd.AddCommand(&cobra.Command{
		Use:   "verify",
		Short: "Verify the registry against the codebase (CI gate)",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := features.Load()
			if err != nil {
				return err
			}
			root, _ := os.Getwd()
			issues := features.Verify(reg, root)
			for _, iss := range issues {
				fmt.Fprintln(os.Stderr, iss)
			}
			if len(issues) > 0 {
				return fmt.Errorf("%d feature registry verification issue(s)", len(issues))
			}
			fmt.Printf("registry: ok (%d features)\n", len(reg.Features))
			return nil
		},
	})

	return cmd
}

package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func newSkillCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "skill", Short: "Inspect skills compiled from agent.yaml"}
	cmd.PersistentFlags().StringVarP(&file, "file", "f", "agent.yaml", "harness configuration file")
	cmd.AddCommand(newSkillListCmd(&file), newSkillShowCmd(&file))
	return cmd
}

func newSkillListCmd(file *string) *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List configured skill resources", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			doc, err := loadHarness(*file)
			if err != nil {
				return err
			}
			names := make([]string, 0, len(doc.Spec.Skills))
			for name := range doc.Spec.Skills {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				skill := doc.Spec.Skills[name]
				fmt.Fprintf(cmd.OutOrStdout(), "%s\tv%d\t%s\t%s\n", name, skill.Version, skill.Source.CatalogRef, join(skill.EffectsCeiling))
			}
			return nil
		},
	}
}

func newSkillShowCmd(file *string) *cobra.Command {
	return &cobra.Command{
		Use: "show <name>", Short: "Show one configured skill resource", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := loadHarness(*file)
			if err != nil {
				return err
			}
			skill, ok := doc.Spec.Skills[args[0]]
			if !ok {
				return fmt.Errorf("compiled harness skill not found: %s", args[0])
			}
			data, err := json.MarshalIndent(skill, "", "  ")
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(append(data, '\n'))
			return err
		},
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/skillengine"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage the skill engine: list, show, and inspect skills",
	}

	cmd.AddCommand(newSkillListCmd())
	cmd.AddCommand(newSkillShowCmd())

	return cmd
}

func newSkillListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered skills",
		RunE: func(_ *cobra.Command, _ []string) error {
			registry, err := loadSkillRegistry()
			if err != nil {
				return err
			}

			skills := registry.List()
			if len(skills) == 0 {
				fmt.Println("No skills registered.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tVERSION\tUSES\tSUCCESS\tDECAYED\tMODE")
			for _, s := range skills {
				fmt.Fprintf(w, "%s\tv%d\t%d\t%.0f%%\t%.2f\t%s\n",
					s.Name,
					s.Version,
					s.Stats.Uses,
					s.Stats.SuccessRate*100,
					s.Stats.DecayedScore,
					s.EvolutionMode,
				)
			}
			return w.Flush()
		},
	}
}

func newSkillShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show details of a specific skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			registry, err := loadSkillRegistry()
			if err != nil {
				return err
			}

			skill, ok := registry.Get(args[0])
			if !ok {
				return fmt.Errorf("skill %q not found", args[0])
			}

			fmt.Printf("Name:        %s\n", skill.Name)
			fmt.Printf("Version:     %d\n", skill.Version)
			fmt.Printf("Description: %s\n", skill.Description)
			fmt.Printf("Mode:        %s\n", skill.EvolutionMode)
			if skill.Parent != "" {
				fmt.Printf("Parent:      %s\n", skill.Parent)
			}
			fmt.Printf("\nPerformance:\n")
			fmt.Printf("  Uses:          %d\n", skill.Stats.Uses)
			fmt.Printf("  Successes:     %d\n", skill.Stats.Successes)
			fmt.Printf("  Failures:      %d\n", skill.Stats.Failures)
			fmt.Printf("  Success Rate:  %.1f%%\n", skill.Stats.SuccessRate*100)
			fmt.Printf("  Decayed Score: %.2f\n", skill.Stats.DecayedScore)
			fmt.Printf("  Avg Duration:  %.0fms\n", skill.Stats.AvgDuration)

			if len(skill.TriggerPatterns) > 0 {
				fmt.Printf("\nTrigger Patterns:\n")
				for _, p := range skill.TriggerPatterns {
					fmt.Printf("  - %s\n", p)
				}
			}
			if len(skill.TriggerKeywords) > 0 {
				fmt.Printf("\nTrigger Keywords:\n")
				for _, k := range skill.TriggerKeywords {
					fmt.Printf("  - %s\n", k)
				}
			}

			return nil
		},
	}
}

func loadSkillRegistry() (*skillengine.Registry, error) {
	home, _ := os.UserHomeDir()
	skillDir := filepath.Join(home, ".agents", "ycode", "skills")
	registry := skillengine.NewRegistry(skillDir)
	if err := registry.LoadFromDir(); err != nil {
		return nil, fmt.Errorf("load skills: %w", err)
	}
	return registry, nil
}

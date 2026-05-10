package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/dhnt/dhnt/catalog"
	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/skillengine"
	"github.com/qiangli/ycode/internal/tools"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "List, show, and inspect skills available to this ycode binary",
	}

	cmd.AddCommand(newSkillListCmd())
	cmd.AddCommand(newSkillShowCmd())

	return cmd
}

// newSkillListCmd renders the three skill sources side by side:
//
//	[external] dhnt catalog (github.com/dhnt/dhnt/catalog) — embedded.
//	[internal] local overlay (.agents/ycode/skills, $YCODE_SKILLS_DIR) — disk.
//	[engine]   skillengine auto-evolution registry — usage-driven.
//
// Mirrors the dispatch precedence in internal/tools/skill.go:resolveSkill.
// The friction this fixes: previously this command only showed the
// engine view, which is empty by default, hiding the 50+ skills the
// LLM actually has access to.
func newSkillListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all skills available to this ycode binary (external catalog + internal lane + auto-evolution engine)",
		RunE: func(_ *cobra.Command, _ []string) error {
			externals := catalog.All()
			internals := tools.ListLocalSkillMeta()

			var engines []*skillengine.SkillSpec
			if registry, err := loadSkillRegistry(); err == nil {
				engines = registry.List()
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)

			// External — community catalog from dhnt module.
			fmt.Fprintf(w, "[external] %d skills from github.com/dhnt/dhnt/catalog\n", len(externals))
			if len(externals) > 0 {
				fmt.Fprintln(w, "  NAME\tPHASE\tEXEC\tDESCRIPTION")
				for _, s := range externals {
					fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
						s.Name, s.Phase, s.Executor, truncateDesc(s.Description, 80))
				}
			}
			fmt.Fprintln(w)

			// Internal — local-overlay disk skills.
			fmt.Fprintf(w, "[internal] %d skills from local overlay (.agents/ycode/skills/, $YCODE_SKILLS_DIR)\n", len(internals))
			if len(internals) == 0 {
				fmt.Fprintln(w, "  (none — add directories under .agents/ycode/skills/<name>/skill.md to extend)")
			} else {
				fmt.Fprintln(w, "  NAME\tDESCRIPTION")
				for _, s := range internals {
					fmt.Fprintf(w, "  %s\t%s\n", s.Name, truncateDesc(s.Description, 80))
				}
			}
			fmt.Fprintln(w)

			// Engine — auto-evolution registry, populated by usage.
			fmt.Fprintf(w, "[engine] %d skills from auto-evolution registry\n", len(engines))
			if len(engines) == 0 {
				fmt.Fprintln(w, "  (registry is populated by skill usage; see internal/runtime/skillengine/)")
			} else {
				fmt.Fprintln(w, "  NAME\tVERSION\tUSES\tSUCCESS\tDECAYED\tMODE")
				for _, s := range engines {
					fmt.Fprintf(w, "  %s\tv%d\t%d\t%.0f%%\t%.2f\t%s\n",
						s.Name, s.Version, s.Stats.Uses,
						s.Stats.SuccessRate*100, s.Stats.DecayedScore,
						s.EvolutionMode)
				}
			}
			return w.Flush()
		},
	}
}

func truncateDesc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
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

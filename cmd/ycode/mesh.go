package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/mesh"
)

func newMeshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mesh",
		Short: "Manage the autonomous agent mesh",
		Long:  "Control background agents that observe, diagnose, learn, and improve ycode.",
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show mesh agent status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mesh.DefaultMeshConfig()
			fmt.Println("Agent Mesh Status")
			fmt.Println(strings.Repeat("─", 40))
			fmt.Printf("Enabled: %v\n", cfg.Enabled)
			fmt.Printf("Mode:    %s\n", cfg.Mode)
			fmt.Println()
			fmt.Println("Agents:")
			if cfg.Mode == "cli" {
				fmt.Println("  diagnoser  (always-on observer)")
				fmt.Println("  learner    (memory consolidation)")
			} else {
				fmt.Println("  diagnoser  (always-on observer)")
				fmt.Println("  fixer      (auto-remediation)")
				fmt.Println("  learner    (memory consolidation)")
				fmt.Println("  researcher (background web research)")
				fmt.Println("  trainer    (scheduled model training)")
			}
			fmt.Println()
			fmt.Println("To enable: set mesh_enabled=true in settings.json")
			return nil
		},
	}

	cmd.AddCommand(statusCmd)
	return cmd
}

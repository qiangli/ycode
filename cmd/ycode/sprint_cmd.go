package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/sprint"
)

func newSprintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sprint [goal]",
		Short: "Run a structured sprint: Plan-Execute-Complete-Reassess-Validate",
		Long: `Sprint decomposes a goal into Milestones, Slices, and Tasks.
Each leaf task fits in one context window and runs with fresh context.

  Plan → Execute → Complete → Reassess → ValidateMilestone → Done

State is persisted to disk, so sprints can resume after interruption.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			goal := strings.Join(args, " ")
			budget, _ := cmd.Flags().GetInt("budget")
			checkCmd, _ := cmd.Flags().GetString("check")
			resumeFlag, _ := cmd.Flags().GetBool("resume")

			deps, err := newRalphDeps(cmd)
			if err != nil {
				return fmt.Errorf("initialize sprint: %w", err)
			}

			home, _ := os.UserHomeDir()
			stateDir := filepath.Join(home, ".agents", "ycode", "sprint-sessions")

			var state *sprint.SprintState
			if resumeFlag {
				state, err = sprint.LoadSprintState(stateDir)
				if err != nil {
					return fmt.Errorf("resume sprint: %w", err)
				}
				slog.Info("sprint: resuming", "phase", state.Phase, "goal", state.Goal)
			} else {
				state = sprint.NewSprintState(goal, budget)
				slog.Info("sprint: starting", "goal", goal, "budget", budget)
			}

			runnerCfg := &sprint.RunnerConfig{
				RalphDeps:    deps,
				CheckCommand: checkCmd,
				StateDir:     stateDir,
			}

			runner := sprint.NewRunner(runnerCfg, state)
			if err := runner.Run(cmd.Context()); err != nil {
				return fmt.Errorf("sprint: %w", err)
			}

			fmt.Printf("Sprint completed: %s\n", state.Phase)
			return nil
		},
	}

	cmd.Flags().String("model", "sonnet", "Model to use")
	cmd.Flags().Int("budget", 0, "Max tokens (0 = unlimited)")
	cmd.Flags().String("check", "", "Verification command")
	cmd.Flags().Bool("resume", false, "Resume from last saved state")

	return cmd
}

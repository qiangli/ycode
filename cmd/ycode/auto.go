package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/autoloop"
)

func newAutoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auto [goal]",
		Short: "Run the autonomous RESEARCH-PLAN-BUILD-EVALUATE-LEARN loop",
		Long: `The autonomous loop iterates through five phases to achieve a goal:
  1. RESEARCH — search for prior work, gaps, and SOTA
  2. PLAN — decompose gaps into prioritized tasks
  3. BUILD — execute tasks via sprint or Ralph
  4. EVALUATE — run eval suite and score
  5. LEARN — extract patterns and persist learnings

Stagnation detection stops the loop if the score plateaus.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			goal := strings.Join(args, " ")
			maxIter, _ := cmd.Flags().GetInt("max-iterations")
			checkCmd, _ := cmd.Flags().GetString("check")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			budget, _ := cmd.Flags().GetInt("budget")
			stagnation, _ := cmd.Flags().GetInt("stagnation-limit")

			deps, err := newRalphDeps(cmd)
			if err != nil {
				return fmt.Errorf("initialize autoloop: %w", err)
			}

			cfg := autoloop.DefaultConfig()
			cfg.Goal = goal
			cfg.MaxIterations = maxIter
			cfg.CheckCommand = checkCmd
			cfg.Timeout = timeout
			cfg.Budget = budget
			cfg.StagnationLimit = stagnation

			// Wire callbacks: research uses web search tool from the registry.
			callbacks := &autoloop.Callbacks{}

			// Build callback: use Ralph step for each task.
			_ = deps // deps available for wiring more callbacks in the future

			slog.Info("autoloop: starting",
				"goal", goal,
				"max_iterations", maxIter,
				"check", checkCmd,
				"budget", budget,
			)

			loop := autoloop.New(cfg, callbacks)
			results, err := loop.Run(cmd.Context())
			if err != nil {
				return fmt.Errorf("autoloop: %w", err)
			}

			fmt.Print(autoloop.FormatSummary(results))
			return nil
		},
	}

	cmd.Flags().String("model", "sonnet", "Model to use")
	cmd.Flags().Int("max-iterations", 5, "Maximum RESEARCH-PLAN-BUILD-EVALUATE-LEARN cycles")
	cmd.Flags().String("check", "", "Evaluation command (e.g., 'go test ./...')")
	cmd.Flags().Duration("timeout", 0, "Overall timeout (0 = no timeout)")
	cmd.Flags().Int("budget", 0, "Max tokens across all cycles (0 = unlimited)")
	cmd.Flags().Int("stagnation-limit", 2, "Stop if score unchanged for N iterations")

	return cmd
}

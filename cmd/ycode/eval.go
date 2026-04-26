package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/eval"
)

func newEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Evaluation framework for agentic capability regression testing",
		Long: `Run evaluation scenarios to measure and track ycode's agentic capabilities.

Tiers:
  contract    No LLM, deterministic tests of agent machinery
  smoke       Real LLM, fast pass@k scenarios
  behavioral  Multi-step trajectory analysis
  e2e         Full coding tasks in sandboxed workspaces`,
	}

	cmd.AddCommand(
		newEvalContractCmd(),
		newEvalReportCmd(),
		newEvalCompareCmd(),
	)

	return cmd
}

// newEvalContractCmd runs contract-tier evaluations (no LLM required).
func newEvalContractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contract",
		Short: "Run contract-tier evals (no LLM, deterministic)",
		Long:  "Runs contract tests that verify agent machinery without calling any LLM provider.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Running contract-tier evaluations...")
			fmt.Println()
			fmt.Println("Contract tests are run via 'go test':")
			fmt.Println("  go test -short -race ./internal/eval/...")
			fmt.Println()
			fmt.Println("To run as part of the build gate:")
			fmt.Println("  make eval-contract")
			return nil
		},
	}
	return cmd
}

// newEvalReportCmd shows the latest eval report or regression analysis.
func newEvalReportCmd() *cobra.Command {
	var reportDir string

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Show latest eval results and regression analysis",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := eval.NewReportStore(reportDir)
			if err != nil {
				return fmt.Errorf("open report store: %w", err)
			}

			reports, err := store.Latest()
			if err != nil {
				return fmt.Errorf("load latest report: %w", err)
			}

			for _, r := range reports {
				fmt.Printf("Run: %s  Version: %s  Provider: %s  Model: %s\n",
					r.Timestamp.Format(time.RFC3339), r.Version, r.Provider, r.Model)
				fmt.Printf("Tier: %s  Composite: %.0f/100\n", r.Tier, r.Composite*100)

				if len(r.Scenarios) > 0 {
					fmt.Println()
					fmt.Printf("  %-30s  pass@k  pass^k  flakiness  tool_acc  traj\n", "Scenario")
					fmt.Println("  " + strings.Repeat("-", 80))
					for _, s := range r.Scenarios {
						fmt.Printf("  %-30s  %.2f    %.2f    %.2f       %.2f      %.2f\n",
							s.Scenario,
							s.Metrics.PassAtK,
							s.Metrics.PassPowK,
							s.Metrics.Flakiness,
							s.Metrics.ToolAccuracy,
							s.Metrics.TrajectoryScore,
						)
					}
				}
				fmt.Println()
			}

			return nil
		},
	}

	defaultDir := defaultReportDir()
	cmd.Flags().StringVar(&reportDir, "dir", defaultDir, "Report storage directory")

	return cmd
}

// newEvalCompareCmd compares two eval report files for regression.
func newEvalCompareCmd() *cobra.Command {
	var reportDir string

	cmd := &cobra.Command{
		Use:   "compare <baseline-file> <current-file>",
		Short: "Compare two eval runs for regression",
		Long:  "Loads two report files and generates a regression analysis.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := eval.NewReportStore(reportDir)
			if err != nil {
				return fmt.Errorf("open report store: %w", err)
			}

			baselineReports, err := store.Load(args[0])
			if err != nil {
				return fmt.Errorf("load baseline %q: %w", args[0], err)
			}
			currentReports, err := store.Load(args[1])
			if err != nil {
				return fmt.Errorf("load current %q: %w", args[1], err)
			}

			if len(baselineReports) == 0 || len(currentReports) == 0 {
				return fmt.Errorf("both files must contain at least one report")
			}

			// Compare the first report from each file.
			output := eval.FormatReport(&baselineReports[0], &currentReports[0])
			fmt.Print(output)

			checks := eval.CompareReports(&baselineReports[0], &currentReports[0])
			if eval.HasRegression(checks) {
				return fmt.Errorf("regression detected")
			}

			return nil
		},
	}

	defaultDir := defaultReportDir()
	cmd.Flags().StringVar(&reportDir, "dir", defaultDir, "Report storage directory")

	return cmd
}

func defaultReportDir() string {
	if dir := os.Getenv("EVAL_REPORT_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".eval-reports")
	}
	return filepath.Join(home, ".local", "share", "ycode", "eval-reports")
}

// registerEvalCmd is called from main to add the eval command tree.
func registerEvalCmd(rootCmd *cobra.Command) {
	rootCmd.AddCommand(newEvalCmd())
}

// runContractEvals is a helper that can be called programmatically
// to check contract-tier scenarios. Used by the eval subcommand
// and by any future integration into the build pipeline.
func runContractEvals(ctx context.Context) error {
	scenarios := contractScenarios()
	if len(scenarios) == 0 {
		fmt.Println("No contract scenarios defined yet.")
		return nil
	}

	runner := eval.NewRunner(eval.RunConfig{
		Provider: "mock",
		Model:    "contract",
		Version:  version,
	}, func(ctx context.Context, s *eval.Scenario) (*eval.RunResult, error) {
		// Contract tests use mock execution — no real LLM.
		return &eval.RunResult{
			Response: "mock response",
			Duration: time.Millisecond,
		}, nil
	})

	var failures int
	for _, s := range scenarios {
		result, err := runner.Run(ctx, s)
		if err != nil {
			fmt.Printf("  FAIL  %s: %v\n", s.Name, err)
			failures++
			continue
		}

		passed := 0
		for _, t := range result.Trials {
			if t.Passed {
				passed++
			}
		}

		if passed >= s.EffectivePassThreshold() {
			fmt.Printf("  PASS  %s (%.0f/100)\n", s.Name, result.Metrics.PassAtK*100)
		} else {
			fmt.Printf("  FAIL  %s (pass@k=%.2f, need %d/%d)\n",
				s.Name, result.Metrics.PassAtK, s.EffectivePassThreshold(), s.EffectiveTrials())
			failures++
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d scenario(s) failed", failures)
	}
	return nil
}

// contractScenarios returns the built-in contract-tier scenarios.
// These test agent machinery without any LLM calls.
func contractScenarios() []*eval.Scenario {
	// Contract scenarios will be populated as they are implemented.
	// For now, the contract tests live in internal/eval/contract/ as Go tests.
	return nil
}

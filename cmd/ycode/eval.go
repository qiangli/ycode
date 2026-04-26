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
		newEvalRunCmd(),
		newEvalMatrixCmd(),
		newEvalScheduleCmd(),
		newEvalReportCmd(),
		newEvalCompareCmd(),
		newEvalHistoryCmd(),
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

// newEvalRunCmd runs eval scenarios across specified tiers.
func newEvalRunCmd() *cobra.Command {
	var tiers []string
	var reportDir string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run eval scenarios (smoke, behavioral, or e2e)",
		Long: `Run evaluation scenarios against a real LLM provider.

Provider is selected via EVAL_PROVIDER env (ollama, anthropic, openai).
Model is selected via EVAL_MODEL env.

Examples:
  EVAL_PROVIDER=ollama ycode eval run --tier smoke
  EVAL_PROVIDER=anthropic ycode eval run --tier smoke --tier behavioral`,
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, model, err := eval.ProviderFromEnv()
			if err != nil {
				return err
			}

			cfg := eval.RunConfig{
				Provider: os.Getenv("EVAL_PROVIDER"),
				Model:    model,
				Version:  version,
			}

			store, err := eval.NewReportStore(reportDir)
			if err != nil {
				return fmt.Errorf("open report store: %w", err)
			}

			runner := eval.AgentRunner(cfg, provider)

			fmt.Printf("Running evals: provider=%s model=%s tiers=%v\n\n", cfg.Provider, model, tiers)

			var allResults []eval.ScenarioResult
			start := time.Now()

			for _, tierName := range tiers {
				fmt.Printf("--- Tier: %s ---\n", tierName)
				scenarios := scenariosForTier(tierName)
				if len(scenarios) == 0 {
					fmt.Printf("  (no scenarios for tier %q — run via 'go test -tags %s')\n\n", tierName, buildTagForTier(tierName))
					continue
				}

				for _, s := range scenarios {
					result, runErr := runner.Run(cmd.Context(), s)
					if runErr != nil {
						fmt.Printf("  ERROR  %s: %v\n", s.Name, runErr)
						continue
					}

					passed := 0
					for _, t := range result.Trials {
						if t.Passed {
							passed++
						}
					}

					status := "PASS"
					if passed < s.EffectivePassThreshold() {
						status = "FAIL"
					}
					fmt.Printf("  %s  %s  pass@k=%.2f  pass^k=%.2f  flakiness=%.2f\n",
						status, s.Name, result.Metrics.PassAtK, result.Metrics.PassPowK, result.Metrics.Flakiness)

					allResults = append(allResults, *result)
				}
				fmt.Println()
			}

			// Save report.
			composite := aggregateComposite(allResults)
			report := &eval.Report{
				ID:        fmt.Sprintf("%s-%s", time.Now().Format("20060102-150405"), version),
				Version:   version,
				Provider:  cfg.Provider,
				Model:     model,
				Tier:      strings.Join(tiers, ","),
				Timestamp: time.Now(),
				Scenarios: allResults,
				Composite: composite,
				Duration:  time.Since(start),
			}

			if err := store.Save(report); err != nil {
				return fmt.Errorf("save report: %w", err)
			}

			fmt.Printf("Composite: %.0f/100  Duration: %s  Report saved.\n", composite*100, time.Since(start).Truncate(time.Second))
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&tiers, "tier", []string{"smoke"}, "Tiers to run (smoke, behavioral, e2e)")
	cmd.Flags().StringVar(&reportDir, "report-dir", defaultReportDir(), "Report storage directory")

	return cmd
}

// newEvalScheduleCmd sets up recurring eval runs.
func newEvalScheduleCmd() *cobra.Command {
	var interval string

	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Schedule recurring eval runs",
		Long: `Schedule eval runs at a recurring interval.

Examples:
  ycode eval schedule --interval 24h
  ycode eval schedule --interval 6h`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dur, err := time.ParseDuration(interval)
			if err != nil {
				return fmt.Errorf("invalid interval %q: %w", interval, err)
			}
			fmt.Printf("Eval schedule configured: every %s\n", dur)
			fmt.Println()
			fmt.Println("To run scheduled evals, use the ycode loop system:")
			fmt.Printf("  ycode loop --interval %s --prompt 'Run eval smoke tier'\n", interval)
			fmt.Println()
			fmt.Println("Or integrate with the CronRegistry in serve mode:")
			fmt.Printf("  Schedule: %s\n", interval)
			fmt.Println("  Command:  eval run --tier smoke --tier behavioral")
			return nil
		},
	}

	cmd.Flags().StringVar(&interval, "interval", "24h", "Run interval (e.g. 6h, 24h)")

	return cmd
}

// newEvalHistoryCmd shows eval score trend over time.
func newEvalHistoryCmd() *cobra.Command {
	var reportDir string
	var limit int

	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show eval score trend over time",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := eval.NewReportStore(reportDir)
			if err != nil {
				return err
			}

			files, err := store.ListFiles()
			if err != nil {
				return err
			}

			if len(files) == 0 {
				fmt.Println("No eval reports found.")
				return nil
			}

			// Show most recent N.
			start := 0
			if len(files) > limit {
				start = len(files) - limit
			}

			fmt.Printf("%-12s  %-8s  %-20s  %-10s  %s\n", "Date", "Score", "Provider/Model", "Tier", "Version")
			fmt.Println(strings.Repeat("-", 75))

			for _, f := range files[start:] {
				reports, err := store.Load(f)
				if err != nil {
					continue
				}
				for _, r := range reports {
					fmt.Printf("%-12s  %5.0f/100  %-20s  %-10s  %s\n",
						r.Timestamp.Format("2006-01-02"),
						r.Composite*100,
						r.Provider+"/"+r.Model,
						r.Tier,
						r.Version,
					)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&reportDir, "dir", defaultReportDir(), "Report storage directory")
	cmd.Flags().IntVar(&limit, "limit", 30, "Number of recent runs to show")

	return cmd
}

// scenariosForTier returns scenarios for a tier name.
// For smoke/behavioral/e2e, scenarios are defined in their respective packages
// and run via go test with build tags. This function returns nil as a placeholder
// — the real scenarios are invoked through the test framework.
func scenariosForTier(tier string) []*eval.Scenario {
	// Scenarios are defined in their respective packages and invoked via go test.
	// This CLI path is for future programmatic invocation.
	return nil
}

func buildTagForTier(tier string) string {
	switch tier {
	case "smoke":
		return "eval"
	case "behavioral":
		return "eval_behavioral"
	case "e2e":
		return "eval_e2e"
	default:
		return tier
	}
}

// newEvalMatrixCmd runs scenarios across multiple providers for comparison.
func newEvalMatrixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "matrix",
		Short: "Run scenarios across multiple providers and compare results",
		Long: `Execute the same eval scenarios against multiple LLM providers
and generate a side-by-side comparison table.

Requires API keys for each provider in environment variables.

Examples:
  ycode eval matrix
  ycode eval matrix --providers ollama,anthropic,openai`,
		RunE: func(cmd *cobra.Command, args []string) error {
			providersFlag, _ := cmd.Flags().GetString("providers")
			providerNames := strings.Split(providersFlag, ",")

			result := &eval.MatrixResult{
				Timestamp: time.Now(),
				Version:   version,
				Entries:   make(map[string]*eval.MatrixProviderResult),
			}

			for _, name := range providerNames {
				name = strings.TrimSpace(name)
				os.Setenv("EVAL_PROVIDER", name)

				provider, model, err := eval.ProviderFromEnv()
				if err != nil {
					fmt.Printf("Skipping %s: %v\n", name, err)
					continue
				}

				key := name + "/" + model
				fmt.Printf("Running scenarios against %s...\n", key)

				cfg := eval.RunConfig{
					Provider: name,
					Model:    model,
					Version:  version,
				}

				runner := eval.AgentRunner(cfg, provider)
				start := time.Now()

				// Run all smoke scenarios (contract tests don't need LLM).
				var scenarios []*eval.Scenario
				// Scenarios are registered via build tags — for CLI matrix,
				// we use a minimal built-in set.
				if len(scenarios) == 0 {
					fmt.Printf("  (no built-in scenarios — run via 'go test -tags eval')\n")
					result.Entries[key] = &eval.MatrixProviderResult{
						Provider: name,
						Model:    model,
						Duration: time.Since(start),
					}
					continue
				}

				var scenarioResults []eval.ScenarioResult
				for _, s := range scenarios {
					sr, err := runner.Run(cmd.Context(), s)
					if err != nil {
						fmt.Printf("  ERROR %s: %v\n", s.Name, err)
						continue
					}
					scenarioResults = append(scenarioResults, *sr)
				}

				composite := aggregateComposite(scenarioResults)
				result.Entries[key] = &eval.MatrixProviderResult{
					Provider:  name,
					Model:     model,
					Composite: composite,
					Scenarios: scenarioResults,
					Duration:  time.Since(start),
				}

				fmt.Printf("  Composite: %.0f/100\n", composite*100)
			}

			fmt.Println()
			fmt.Print(eval.FormatMatrix(result))

			return nil
		},
	}

	cmd.Flags().String("providers", "ollama,anthropic", "Comma-separated providers to compare")

	return cmd
}

func aggregateComposite(results []eval.ScenarioResult) float64 {
	if len(results) == 0 {
		return 0
	}
	var totalPassAtK, totalPassPowK, totalFlakiness float64
	for _, r := range results {
		totalPassAtK += r.Metrics.PassAtK
		totalPassPowK += r.Metrics.PassPowK
		totalFlakiness += r.Metrics.Flakiness
	}
	n := float64(len(results))
	return eval.CompositeScore(
		totalPassAtK/n,
		totalPassPowK/n,
		totalFlakiness/n,
		1.0, 1.0,
	)
}

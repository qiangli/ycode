package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/ralph"
)

func newRalphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ralph [prompt]",
		Short: "Run the Ralph autonomous loop (step → check → commit → repeat)",
		Long: `Ralph is an autonomous iterative agent loop that:
  1. Executes a step (LLM call with the given prompt)
  2. Runs a check command (e.g., go test ./...)
  3. Commits on success (optional)
  4. Repeats until target score reached or max iterations

Eval-driven termination, stagnation detection, and automatic checkpointing.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.Join(args, " ")
			maxIter, _ := cmd.Flags().GetInt("max-iterations")
			targetScore, _ := cmd.Flags().GetFloat64("target-score")
			checkCmd, _ := cmd.Flags().GetString("check")
			commitOnSuccess, _ := cmd.Flags().GetBool("commit")
			commitMsg, _ := cmd.Flags().GetString("commit-message")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			prdPath, _ := cmd.Flags().GetString("prd")

			// Resolve provider.
			model, _ := cmd.Flags().GetString("model")
			if model == "" {
				model = "sonnet"
			}
			resolved := api.ResolveModelWithAliases(model, nil)
			providerCfg, err := api.DetectProvider(resolved)
			if err != nil {
				return fmt.Errorf("detect provider: %w", err)
			}
			provider := api.NewProvider(providerCfg)

			cfg := ralph.DefaultConfig()
			cfg.MaxIterations = maxIter
			cfg.TargetScore = targetScore
			cfg.CommitOnSuccess = commitOnSuccess
			cfg.CommitMessage = commitMsg
			cfg.Timeout = timeout
			cfg.PRDPath = prdPath

			ctrl := ralph.NewController(cfg, func(ctx context.Context, state *ralph.State, iteration int) (string, float64, error) {
				// Single-shot LLM call for each iteration.
				iterPrompt := fmt.Sprintf("Iteration %d/%d.\n\n%s", iteration, maxIter, prompt)
				if state.LastError != "" {
					iterPrompt += fmt.Sprintf("\n\nPrevious error: %s", state.LastError)
				}
				if state.LastCheckOutput != "" {
					iterPrompt += fmt.Sprintf("\n\nPrevious check output:\n%s", state.LastCheckOutput)
				}

				req := &api.Request{
					Model:     resolved,
					MaxTokens: 8192,
					System:    "You are an autonomous coding agent. Execute the task and write code directly.",
					Messages: []api.Message{{
						Role: api.RoleUser,
						Content: []api.ContentBlock{
							{Type: api.ContentTypeText, Text: iterPrompt},
						},
					}},
					Stream: true,
				}

				events, errc := provider.Send(ctx, req)
				var textParts []string
				for ev := range events {
					if ev.Delta != nil {
						var delta struct{ Text string }
						if jsonErr := parseJSONDelta(ev.Delta, &delta); jsonErr == nil && delta.Text != "" {
							textParts = append(textParts, delta.Text)
							fmt.Print(delta.Text) // Stream to terminal.
						}
					}
				}
				if streamErr := <-errc; streamErr != nil {
					return "", 0, streamErr
				}
				fmt.Println()

				return strings.Join(textParts, ""), 0, nil
			})

			// Wire check function.
			if checkCmd != "" {
				ctrl.SetCheck(func(ctx context.Context) (bool, string, error) {
					out, err := exec.CommandContext(ctx, "sh", "-c", checkCmd).CombinedOutput()
					passed := err == nil
					return passed, string(out), nil
				})
			}

			// Wire commit function.
			if commitOnSuccess {
				ctrl.SetCommit(func(ctx context.Context, message string) error {
					out, err := exec.CommandContext(ctx, "git", "add", "-A").CombinedOutput()
					if err != nil {
						return fmt.Errorf("git add: %s", out)
					}
					out, err = exec.CommandContext(ctx, "git", "commit", "-m", message).CombinedOutput()
					if err != nil {
						return fmt.Errorf("git commit: %s", out)
					}
					slog.Info("ralph: committed", "message", message)
					return nil
				})
			}

			return ctrl.Run(cmd.Context())
		},
	}

	cmd.Flags().String("model", "sonnet", "Model to use")
	cmd.Flags().Int("max-iterations", 10, "Maximum iterations")
	cmd.Flags().Float64("target-score", 0, "Target score to stop (0 = disabled)")
	cmd.Flags().String("check", "", "Check command to run after each step (e.g., 'go test ./...')")
	cmd.Flags().Bool("commit", false, "Auto-commit on success")
	cmd.Flags().String("commit-message", "ralph: automated iteration", "Commit message template")
	cmd.Flags().Duration("timeout", 0, "Overall timeout (0 = no timeout)")
	cmd.Flags().String("prd", "", "Path to prd.json for story-driven mode")

	return cmd
}

// parseJSONDelta is a helper to unmarshal a delta JSON payload.
func parseJSONDelta(data []byte, v any) error {
	if len(data) == 0 {
		return fmt.Errorf("empty delta")
	}
	return json.Unmarshal(data, v)
}

package main

import (
	"context"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/batch"
	"github.com/spf13/cobra"
)

func newBatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Stub: batch prompt runner skeleton (provider not yet wired)",
		Long: `Skeleton for a batch prompt runner. The runner + JSONL plumbing exist,
but the per-row Execute callback returns "batch execute: not yet wired to
provider" — every row will error until a provider is plugged in.`,
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Iterate the JSONL input — each row currently errors with 'not yet wired to provider'",
		RunE: func(cmd *cobra.Command, args []string) error {
			input, _ := cmd.Flags().GetString("input")
			output, _ := cmd.Flags().GetString("output")
			checkpoint, _ := cmd.Flags().GetString("checkpoint")
			concurrency, _ := cmd.Flags().GetInt("concurrency")

			if input == "" {
				return fmt.Errorf("--input is required")
			}
			if output == "" {
				output = "batch_output.jsonl"
			}

			runner := batch.NewRunner(batch.RunnerConfig{
				InputPath:      input,
				OutputPath:     output,
				CheckpointPath: checkpoint,
				Concurrency:    concurrency,
				Execute: func(ctx context.Context, prompt, model string) (string, int, error) {
					// Direct single-shot LLM call for batch processing.
					// Each prompt runs independently without tool use.
					return prompt, 0, fmt.Errorf("batch execute: not yet wired to provider")
				},
			})
			fmt.Printf("Batch runner: input=%s output=%s concurrency=%d\n", input, output, concurrency)
			return runner.Run(cmd.Context())
		},
	}
	runCmd.Flags().String("input", "", "Input JSONL file with prompts")
	runCmd.Flags().String("output", "batch_output.jsonl", "Output JSONL file for results")
	runCmd.Flags().String("checkpoint", "", "Checkpoint file for resume")
	runCmd.Flags().Int("concurrency", 4, "Max parallel prompts")

	cmd.AddCommand(runCmd)
	return cmd
}

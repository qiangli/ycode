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
		Short: "Run batch processing of prompts",
		Long:  "Execute multiple prompts from a JSONL file with checkpointing and statistics.",
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run a batch of prompts",
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

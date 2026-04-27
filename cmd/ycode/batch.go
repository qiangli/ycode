package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/qiangli/ycode/internal/runtime/batch"
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
			})
			_ = runner // TODO: wire up runner.Run() when conversation runtime integration is ready
			fmt.Printf("Batch runner configured: input=%s output=%s concurrency=%d\n", input, output, concurrency)
			return nil
		},
	}
	runCmd.Flags().String("input", "", "Input JSONL file with prompts")
	runCmd.Flags().String("output", "batch_output.jsonl", "Output JSONL file for results")
	runCmd.Flags().String("checkpoint", "", "Checkpoint file for resume")
	runCmd.Flags().Int("concurrency", 4, "Max parallel prompts")

	cmd.AddCommand(runCmd)
	return cmd
}

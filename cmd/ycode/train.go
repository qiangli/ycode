package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTrainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "train",
		Short: "Training and evaluation commands",
		Long:  "Train, evaluate, and collect data for local model improvement.",
	}

	rlCmd := &cobra.Command{
		Use:   "rl",
		Short: "Run GRPO reinforcement learning",
		RunE: func(cmd *cobra.Command, args []string) error {
			task, _ := cmd.Flags().GetString("task")
			model, _ := cmd.Flags().GetString("model")
			steps, _ := cmd.Flags().GetInt("steps")
			fmt.Printf("RL training: task=%s model=%s steps=%d\n", task, model, steps)
			fmt.Println("TODO: wire up GRPOTrainer when Python worker is ready")
			return nil
		},
	}
	rlCmd.Flags().String("task", "gsm8k", "Training task")
	rlCmd.Flags().String("model", "", "Model path or name")
	rlCmd.Flags().Int("steps", 500, "Total training steps")

	collectCmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect training trajectories",
		RunE: func(cmd *cobra.Command, args []string) error {
			task, _ := cmd.Flags().GetString("task")
			output, _ := cmd.Flags().GetString("output")
			count, _ := cmd.Flags().GetInt("count")
			fmt.Printf("Collecting trajectories: task=%s output=%s count=%d\n", task, output, count)
			fmt.Println("TODO: wire up TrajectoryCollector")
			return nil
		},
	}
	collectCmd.Flags().String("task", "terminal", "Task name")
	collectCmd.Flags().String("output", "trajectories.jsonl", "Output JSONL file")
	collectCmd.Flags().Int("count", 10, "Number of trajectories")

	evalCmd := &cobra.Command{
		Use:   "eval",
		Short: "Evaluate a model on a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			task, _ := cmd.Flags().GetString("task")
			model, _ := cmd.Flags().GetString("model")
			samples, _ := cmd.Flags().GetInt("samples")
			fmt.Printf("Evaluating: task=%s model=%s samples=%d\n", task, model, samples)
			fmt.Println("TODO: wire up task evaluation")
			return nil
		},
	}
	evalCmd.Flags().String("task", "gsm8k", "Task name")
	evalCmd.Flags().String("model", "", "Model to evaluate")
	evalCmd.Flags().Int("samples", 50, "Number of examples")

	cmd.AddCommand(rlCmd, collectCmd, evalCmd)
	return cmd
}

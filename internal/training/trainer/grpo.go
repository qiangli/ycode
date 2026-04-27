package trainer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"

	"github.com/qiangli/ycode/internal/training/rollout"
)

// GRPOConfig configures the GRPO training orchestrator.
type GRPOConfig struct {
	PythonBin    string // path to python binary (default: "python3")
	ScriptPath   string // path to GRPO training script
	ModelPath    string // path to model checkpoint
	OutputDir    string // directory for training outputs
	LearningRate float64
	BatchSize    int
	TotalSteps   int
	GroupSize    int // number of samples per example for GRPO
}

// GRPOTrainer orchestrates RL training via a Python subprocess.
// Go handles trajectory collection; Python handles GPU training.
type GRPOTrainer struct {
	config GRPOConfig
	logger *slog.Logger
}

// NewGRPOTrainer creates a GRPO training orchestrator.
func NewGRPOTrainer(cfg GRPOConfig) *GRPOTrainer {
	if cfg.PythonBin == "" {
		cfg.PythonBin = "python3"
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 4
	}
	if cfg.GroupSize <= 0 {
		cfg.GroupSize = 4
	}
	if cfg.LearningRate <= 0 {
		cfg.LearningRate = 1e-5
	}
	return &GRPOTrainer{
		config: cfg,
		logger: slog.Default(),
	}
}

// TrainStep sends a batch of trajectories to the Python trainer.
// Returns the training loss for this step.
func (t *GRPOTrainer) TrainStep(ctx context.Context, trajectories []rollout.ScoredTrajectory) (*StepResult, error) {
	if t.config.ScriptPath == "" {
		return nil, fmt.Errorf("GRPO script path not configured")
	}

	// Serialize trajectories as JSON to stdin.
	data, err := json.Marshal(trajectories)
	if err != nil {
		return nil, fmt.Errorf("marshal trajectories: %w", err)
	}

	cmd := exec.CommandContext(ctx, t.config.PythonBin, t.config.ScriptPath,
		"--model", t.config.ModelPath,
		"--output", t.config.OutputDir,
		fmt.Sprintf("--lr=%g", t.config.LearningRate),
		fmt.Sprintf("--batch-size=%d", t.config.BatchSize),
		fmt.Sprintf("--group-size=%d", t.config.GroupSize),
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start trainer: %w", err)
	}

	// Send trajectories.
	if _, err := stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write trajectories: %w", err)
	}
	stdin.Close()

	// Read result.
	output, err := io.ReadAll(stdout)
	if err != nil {
		return nil, fmt.Errorf("read output: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("trainer exited: %w", err)
	}

	var result StepResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.logger.Warn("failed to parse trainer output", "raw", string(output))
		return &StepResult{}, nil
	}

	return &result, nil
}

// StepResult is the output from a single training step.
type StepResult struct {
	Loss         float64 `json:"loss"`
	LearningRate float64 `json:"learning_rate"`
	GradNorm     float64 `json:"grad_norm"`
	StepTime     float64 `json:"step_time_s"`
}

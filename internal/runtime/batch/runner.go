package batch

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// BatchPrompt is a single prompt in a batch input file.
type BatchPrompt struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"` // optional per-prompt model override
}

// BatchResult is the output for a single prompt.
type BatchResult struct {
	ID         string  `json:"id"`
	Prompt     string  `json:"prompt"`
	Response   string  `json:"response,omitempty"`
	Error      string  `json:"error,omitempty"`
	Tokens     int     `json:"tokens"`
	CostUSD    float64 `json:"cost_usd"`
	DurationMs int64   `json:"duration_ms"`
}

// BatchStats aggregates statistics across all prompts.
type BatchStats struct {
	Total         int            `json:"total"`
	Completed     int            `json:"completed"`
	Failed        int            `json:"failed"`
	TotalTokens   int            `json:"total_tokens"`
	TotalCostUSD  float64        `json:"total_cost_usd"`
	AvgDurationMs int64          `json:"avg_duration_ms"`
	ToolUsage     map[string]int `json:"tool_usage"`
}

// RunnerConfig configures the batch runner.
type RunnerConfig struct {
	InputPath      string // JSONL file with BatchPrompt entries
	OutputPath     string // JSONL file for BatchResult entries
	CheckpointPath string // checkpoint state file for resume
	Concurrency    int    // max parallel prompts
	MaxRetries     int    // retries per prompt on failure
}

// Runner executes batch prompts with checkpointing.
type Runner struct {
	config RunnerConfig
	stats  BatchStats
	mu     sync.Mutex
}

// NewRunner creates a batch runner.
func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 2
	}
	return &Runner{
		config: cfg,
		stats: BatchStats{
			ToolUsage: make(map[string]int),
		},
	}
}

// LoadPrompts reads prompts from a JSONL file.
func LoadPrompts(path string) ([]BatchPrompt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read prompts: %w", err)
	}
	var prompts []BatchPrompt
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var p BatchPrompt
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("decode prompt: %w", err)
		}
		prompts = append(prompts, p)
	}
	return prompts, nil
}

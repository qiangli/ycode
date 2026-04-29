package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
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

// PromptFunc executes a single prompt and returns the response text.
// The model parameter is optional; if empty, the default model is used.
type PromptFunc func(ctx context.Context, prompt, model string) (response string, tokens int, err error)

// RunnerConfig configures the batch runner.
type RunnerConfig struct {
	InputPath      string     // JSONL file with BatchPrompt entries
	OutputPath     string     // JSONL file for BatchResult entries
	CheckpointPath string     // checkpoint state file for resume
	Concurrency    int        // max parallel prompts
	MaxRetries     int        // retries per prompt on failure
	Execute        PromptFunc // function that executes a single prompt
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

// Run executes all prompts with bounded parallelism and checkpointing.
func (r *Runner) Run(ctx context.Context) error {
	prompts, err := LoadPrompts(r.config.InputPath)
	if err != nil {
		return err
	}
	r.stats.Total = len(prompts)

	if r.config.Execute == nil {
		return fmt.Errorf("no Execute function configured")
	}

	// Load checkpoint to skip already-completed prompts.
	checkpoint, err := NewCheckpoint(r.config.CheckpointPath)
	if err != nil {
		return fmt.Errorf("load checkpoint: %w", err)
	}
	if len(checkpoint.CompletedIDs) > 0 {
		slog.Info("batch: resumed from checkpoint", "completed", len(checkpoint.CompletedIDs))
	}

	// Open output file for append.
	outFile, err := os.OpenFile(r.config.OutputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open output: %w", err)
	}
	defer outFile.Close()
	enc := json.NewEncoder(outFile)

	sem := make(chan struct{}, r.config.Concurrency)
	var wg sync.WaitGroup

	for _, prompt := range prompts {
		if checkpoint.IsCompleted(prompt.ID) {
			r.mu.Lock()
			r.stats.Completed++
			r.mu.Unlock()
			continue
		}

		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(p BatchPrompt) {
			defer wg.Done()
			defer func() { <-sem }()

			result := r.executeWithRetry(ctx, p)

			r.mu.Lock()
			if result.Error != "" {
				r.stats.Failed++
			} else {
				r.stats.Completed++
			}
			r.stats.TotalTokens += result.Tokens
			r.stats.TotalCostUSD += result.CostUSD
			_ = enc.Encode(result)
			r.mu.Unlock()

			// Save checkpoint after each result.
			_ = checkpoint.MarkCompleted(result.ID)
		}(prompt)
	}

	wg.Wait()

	slog.Info("batch: complete",
		"total", r.stats.Total,
		"completed", r.stats.Completed,
		"failed", r.stats.Failed,
		"tokens", r.stats.TotalTokens,
	)
	return nil
}

// executeWithRetry runs a prompt with retries on failure.
func (r *Runner) executeWithRetry(ctx context.Context, p BatchPrompt) BatchResult {
	var lastErr error
	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		start := time.Now()
		response, tokens, err := r.config.Execute(ctx, p.Prompt, p.Model)
		duration := time.Since(start)

		if err == nil {
			return BatchResult{
				ID:         p.ID,
				Prompt:     p.Prompt,
				Response:   response,
				Tokens:     tokens,
				DurationMs: duration.Milliseconds(),
			}
		}
		lastErr = err
		slog.Debug("batch: retry", "id", p.ID, "attempt", attempt+1, "error", err)
	}
	return BatchResult{
		ID:     p.ID,
		Prompt: p.Prompt,
		Error:  lastErr.Error(),
	}
}

// Stats returns the current batch statistics.
func (r *Runner) Stats() BatchStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

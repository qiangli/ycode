package loop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// IterationContext holds state carried between loop iterations.
type IterationContext struct {
	Iteration      int       `json:"iteration"`
	StartedAt      time.Time `json:"started_at"`
	LastRunAt      time.Time `json:"last_run_at"`
	LastDuration   string    `json:"last_duration"`
	TotalRuns      int       `json:"total_runs"`
	SuccessCount   int       `json:"success_count"`
	FailureCount   int       `json:"failure_count"`
	LastError      string    `json:"last_error,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	Improvements   []string  `json:"improvements,omitempty"`
}

// ContextCarryover manages state persistence between loop iterations.
type ContextCarryover struct {
	dir  string
	ctx  *IterationContext
}

// NewContextCarryover creates a new context carryover manager.
func NewContextCarryover(dir string) (*ContextCarryover, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create loop context dir: %w", err)
	}
	cc := &ContextCarryover{dir: dir}
	cc.ctx = cc.load()
	return cc, nil
}

// BeforeRun updates context before an iteration runs.
func (cc *ContextCarryover) BeforeRun(iteration int) {
	cc.ctx.Iteration = iteration
	cc.ctx.TotalRuns++
	if cc.ctx.StartedAt.IsZero() {
		cc.ctx.StartedAt = time.Now()
	}
}

// AfterRun updates context after an iteration completes.
func (cc *ContextCarryover) AfterRun(duration time.Duration, err error) {
	cc.ctx.LastRunAt = time.Now()
	cc.ctx.LastDuration = duration.String()
	if err != nil {
		cc.ctx.FailureCount++
		cc.ctx.LastError = err.Error()
	} else {
		cc.ctx.SuccessCount++
		cc.ctx.LastError = ""
	}
	_ = cc.save()
}

// AddImprovement records an improvement metric.
func (cc *ContextCarryover) AddImprovement(description string) {
	cc.ctx.Improvements = append(cc.ctx.Improvements, description)
	if len(cc.ctx.Improvements) > 20 {
		cc.ctx.Improvements = cc.ctx.Improvements[len(cc.ctx.Improvements)-20:]
	}
}

// SetSessionID sets the session ID for continuation.
func (cc *ContextCarryover) SetSessionID(id string) {
	cc.ctx.SessionID = id
}

// Context returns the current iteration context.
func (cc *ContextCarryover) Context() *IterationContext {
	return cc.ctx
}

// Summary returns a text summary of loop progress.
func (cc *ContextCarryover) Summary() string {
	if cc.ctx.TotalRuns == 0 {
		return "No iterations completed yet."
	}
	return fmt.Sprintf("Loop: %d iterations (%d success, %d failed), running since %s, last run: %s (%s)",
		cc.ctx.TotalRuns, cc.ctx.SuccessCount, cc.ctx.FailureCount,
		cc.ctx.StartedAt.Format(time.RFC3339),
		cc.ctx.LastRunAt.Format(time.RFC3339),
		cc.ctx.LastDuration)
}

func (cc *ContextCarryover) path() string {
	return filepath.Join(cc.dir, "loop_context.json")
}

func (cc *ContextCarryover) save() error {
	data, err := json.MarshalIndent(cc.ctx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cc.path(), data, 0o644)
}

func (cc *ContextCarryover) load() *IterationContext {
	data, err := os.ReadFile(cc.path())
	if err != nil {
		return &IterationContext{}
	}
	var ctx IterationContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return &IterationContext{}
	}
	return &ctx
}

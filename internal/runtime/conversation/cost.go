package conversation

import (
	"fmt"
	"sync"

	"github.com/qiangli/ycode/internal/api"
)

// CostTracker enforces LLM call budgets and tracks cumulative token usage
// across a conversation or invocation. Inspired by ADK-Python's
// _InvocationCostManager with max_llm_calls limits.
type CostTracker struct {
	mu sync.Mutex

	// MaxLLMCalls is the maximum number of LLM calls allowed.
	// 0 means unlimited (no budget enforcement).
	MaxLLMCalls int

	// Cumulative counters.
	LLMCallCount      int
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCacheRead    int
	TotalCacheCreate  int
}

// NewCostTracker creates a cost tracker with the given budget.
// A maxCalls of 0 means unlimited.
func NewCostTracker(maxCalls int) *CostTracker {
	return &CostTracker{MaxLLMCalls: maxCalls}
}

// RecordCall records a completed LLM call with its token usage.
func (ct *CostTracker) RecordCall(usage api.Usage) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.LLMCallCount++
	ct.TotalInputTokens += usage.InputTokens + usage.PromptTokens
	ct.TotalOutputTokens += usage.OutputTokens + usage.CompletionTokens
	ct.TotalCacheRead += usage.CacheReadInput
	ct.TotalCacheCreate += usage.CacheCreationInput
}

// BudgetExceeded returns true if the LLM call budget has been exceeded.
// Always returns false when MaxLLMCalls is 0 (unlimited).
func (ct *CostTracker) BudgetExceeded() bool {
	if ct.MaxLLMCalls <= 0 {
		return false
	}
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.LLMCallCount >= ct.MaxLLMCalls
}

// BudgetError returns an error describing the budget exhaustion, or nil.
func (ct *CostTracker) BudgetError() error {
	if !ct.BudgetExceeded() {
		return nil
	}
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return fmt.Errorf("LLM call budget exceeded: %d/%d calls used (%d input tokens, %d output tokens)",
		ct.LLMCallCount, ct.MaxLLMCalls, ct.TotalInputTokens, ct.TotalOutputTokens)
}

// Snapshot returns a point-in-time copy of the cost statistics.
func (ct *CostTracker) Snapshot() CostSnapshot {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return CostSnapshot{
		LLMCallCount:      ct.LLMCallCount,
		MaxLLMCalls:       ct.MaxLLMCalls,
		TotalInputTokens:  ct.TotalInputTokens,
		TotalOutputTokens: ct.TotalOutputTokens,
		TotalCacheRead:    ct.TotalCacheRead,
		TotalCacheCreate:  ct.TotalCacheCreate,
	}
}

// Reset clears all counters.
func (ct *CostTracker) Reset() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.LLMCallCount = 0
	ct.TotalInputTokens = 0
	ct.TotalOutputTokens = 0
	ct.TotalCacheRead = 0
	ct.TotalCacheCreate = 0
}

// CostSnapshot is a point-in-time copy of cost statistics.
type CostSnapshot struct {
	LLMCallCount      int
	MaxLLMCalls       int
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCacheRead    int
	TotalCacheCreate  int
}

// TotalTokens returns the total token consumption.
func (s CostSnapshot) TotalTokens() int {
	return s.TotalInputTokens + s.TotalOutputTokens
}

// BudgetRemaining returns the number of LLM calls remaining, or -1 if unlimited.
func (s CostSnapshot) BudgetRemaining() int {
	if s.MaxLLMCalls <= 0 {
		return -1
	}
	remaining := s.MaxLLMCalls - s.LLMCallCount
	if remaining < 0 {
		return 0
	}
	return remaining
}

package usage

import (
	"fmt"
	"sync"
)

// Tracker tracks token usage and costs across a session.
type Tracker struct {
	mu sync.Mutex

	InputTokens        int
	OutputTokens       int
	CacheCreationInput int
	CacheReadInput     int

	TotalRequests int
	Model         string
}

// NewTracker creates a new usage tracker.
func NewTracker() *Tracker {
	return &Tracker{}
}

// Add records token usage from a single request.
func (t *Tracker) Add(input, output, cacheCreate, cacheRead int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.InputTokens += input
	t.OutputTokens += output
	t.CacheCreationInput += cacheCreate
	t.CacheReadInput += cacheRead
	t.TotalRequests++
}

// AddWithModel records token usage with model-specific pricing.
func (t *Tracker) AddWithModel(model string, input, output, cacheCreate, cacheRead int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.InputTokens += input
	t.OutputTokens += output
	t.CacheCreationInput += cacheCreate
	t.CacheReadInput += cacheRead
	t.TotalRequests++
	if model != "" {
		t.Model = model
	}
}

// Cost estimates the cost in USD based on Claude Sonnet pricing.
func (t *Tracker) Cost() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.costLocked()
}

func (t *Tracker) costLocked() float64 {
	if t.Model != "" {
		return EstimateCost(t.Model, t.InputTokens, t.OutputTokens, t.CacheCreationInput, t.CacheReadInput)
	}
	// Fallback: hardcoded Claude Sonnet pricing for backward compatibility.
	inputCost := float64(t.InputTokens) * 3.0 / 1_000_000
	outputCost := float64(t.OutputTokens) * 15.0 / 1_000_000
	cacheWriteCost := float64(t.CacheCreationInput) * 3.75 / 1_000_000
	cacheReadCost := float64(t.CacheReadInput) * 0.30 / 1_000_000
	return inputCost + outputCost + cacheWriteCost + cacheReadCost
}

// Summary returns a formatted usage summary.
func (t *Tracker) Summary() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	cost := t.costLocked()
	return fmt.Sprintf("Requests: %d | Input: %d | Output: %d | Cache: %d create, %d read | Est. cost: $%.4f",
		t.TotalRequests, t.InputTokens, t.OutputTokens, t.CacheCreationInput, t.CacheReadInput, cost)
}

// TotalTokens returns total tokens consumed.
func (t *Tracker) TotalTokens() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.InputTokens + t.OutputTokens
}

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
// Add records token usage WITHOUT a model, and therefore prices it against the fallback.
//
// Deprecated: use AddWithModel. Every production call site used this one, so t.Model was
// never set and every session was priced as Claude Sonnet. Kept only so an out-of-tree
// caller does not break; it now records the model as "" and LookupPricing reports
// Known=false, so at least the guess ADMITS it is a guess.
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

// costLocked prices the session against the model that actually ran it.
//
// It used to carry a THIRD hardcoded copy of Claude Sonnet's price ($3/$15 per million),
// reached whenever t.Model was empty — and t.Model was ALWAYS empty, because every
// production call site used Add() rather than AddWithModel() and Add() never set it.
//
// So the "Est. cost: $X" line at the end of every run was computed as if every model on
// earth were Claude Sonnet. GLM-5.2 reported at 5.5x its real price. It did not even read
// the PricingTable — it inlined the numbers.
//
// Found by an adversarial review (glm-5.2). It is the same bug as the metric path, in a
// second cost implementation nobody remembered — which is the "two constructors of one
// thing" trap, for the third time this week.
//
// There is now ONE pricing source: PricingTable, via LookupPricing.
func (t *Tracker) costLocked() float64 {
	return EstimateCost(t.Model, t.InputTokens, t.OutputTokens, t.CacheCreationInput, t.CacheReadInput)
}

// CostIsGuess reports whether the session cost was priced from a DECLARED rate or from the
// fallback guess (which is Claude's rate — so an unknown model reads as expensive, not as
// unknown). A dollar figure nobody can tell apart from a measurement is worse than none.
func (t *Tracker) CostIsGuess() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !LookupPricing(t.Model).Known
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

// HasRequests returns true if any requests have been tracked.
func (t *Tracker) HasRequests() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.TotalRequests > 0
}

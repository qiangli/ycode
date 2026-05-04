package agentpool

import "sync"

// CapacityGovernor enforces a maximum number of concurrent active agents.
// Inspired by gastown's max_polecats capacity governor and paperclip's
// routine scheduling with catch-up limits.
type CapacityGovernor struct {
	mu            sync.Mutex
	maxConcurrent int
	pool          *Pool
}

// NewCapacityGovernor creates a governor that limits concurrent agents.
// A maxConcurrent of 0 means unlimited.
func NewCapacityGovernor(pool *Pool, maxConcurrent int) *CapacityGovernor {
	return &CapacityGovernor{
		pool:          pool,
		maxConcurrent: maxConcurrent,
	}
}

// CanSpawn returns true if a new agent can be spawned without exceeding
// the capacity limit. Returns true if maxConcurrent is 0 (unlimited).
func (g *CapacityGovernor) CanSpawn() bool {
	if g.maxConcurrent <= 0 {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pool.ActiveCount() < g.maxConcurrent
}

// Remaining returns how many more agents can be spawned.
// Returns -1 if unlimited.
func (g *CapacityGovernor) Remaining() int {
	if g.maxConcurrent <= 0 {
		return -1
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	remaining := g.maxConcurrent - g.pool.ActiveCount()
	if remaining < 0 {
		return 0
	}
	return remaining
}

// MaxConcurrent returns the configured limit.
func (g *CapacityGovernor) MaxConcurrent() int {
	return g.maxConcurrent
}

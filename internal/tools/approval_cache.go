package tools

import (
	"sync"
	"time"
)

// ApprovalCache provides session-scoped caching of user approvals for tool invocations.
// Once a user approves a tool, subsequent invocations within the TTL skip the prompt.
type ApprovalCache struct {
	cache map[string]time.Time
	ttl   time.Duration
	mu    sync.RWMutex
}

// NewApprovalCache creates an approval cache with the given TTL.
func NewApprovalCache(ttl time.Duration) *ApprovalCache {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &ApprovalCache{
		cache: make(map[string]time.Time),
		ttl:   ttl,
	}
}

// IsApproved returns true if the tool has a valid (non-expired) cached approval.
func (c *ApprovalCache) IsApproved(toolName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ts, ok := c.cache[toolName]
	if !ok {
		return false
	}
	return time.Since(ts) < c.ttl
}

// Record stores an approval for the given tool.
func (c *ApprovalCache) Record(toolName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[toolName] = time.Now()
}

// RecordMultiple stores approvals for multiple tools at once.
func (c *ApprovalCache) RecordMultiple(toolNames []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for _, name := range toolNames {
		c.cache[name] = now
	}
}

// Clear removes all cached approvals.
func (c *ApprovalCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]time.Time)
}

// Size returns the number of cached approvals.
func (c *ApprovalCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

package swarm

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// ContextVars holds shared state that flows between agents during handoffs.
// It is safe for concurrent use.
type ContextVars struct {
	mu   sync.RWMutex
	vars map[string]string
}

// NewContextVars creates an empty ContextVars.
func NewContextVars() *ContextVars {
	return &ContextVars{
		vars: make(map[string]string),
	}
}

// NewContextVarsFrom creates ContextVars from an existing map.
func NewContextVarsFrom(m map[string]string) *ContextVars {
	cv := NewContextVars()
	for k, v := range m {
		cv.vars[k] = v
	}
	return cv
}

// Get returns the value for the given key.
func (cv *ContextVars) Get(key string) (string, bool) {
	cv.mu.RLock()
	defer cv.mu.RUnlock()
	v, ok := cv.vars[key]
	return v, ok
}

// Set sets a key-value pair.
func (cv *ContextVars) Set(key, value string) {
	cv.mu.Lock()
	defer cv.mu.Unlock()
	cv.vars[key] = value
}

// Merge adds all key-value pairs from the given map.
// Existing keys are overwritten.
func (cv *ContextVars) Merge(m map[string]string) {
	cv.mu.Lock()
	defer cv.mu.Unlock()
	for k, v := range m {
		cv.vars[k] = v
	}
}

// Snapshot returns a copy of all variables.
func (cv *ContextVars) Snapshot() map[string]string {
	cv.mu.RLock()
	defer cv.mu.RUnlock()
	snap := make(map[string]string, len(cv.vars))
	for k, v := range cv.vars {
		snap[k] = v
	}
	return snap
}

// FormatForPrompt renders context variables as a string suitable for injection
// into an agent's system prompt.
func (cv *ContextVars) FormatForPrompt() string {
	cv.mu.RLock()
	defer cv.mu.RUnlock()
	if len(cv.vars) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Swarm Context Variables\n\n")
	for k, v := range cv.vars {
		fmt.Fprintf(&sb, "- **%s**: %s\n", k, v)
	}
	return sb.String()
}

// contextVarsKey is the context key for swarm context variables.
type contextVarsKey struct{}

// WithContextVars adds context variables to a Go context.
func WithContextVars(ctx context.Context, cv *ContextVars) context.Context {
	return context.WithValue(ctx, contextVarsKey{}, cv)
}

// GetContextVars retrieves context variables from a Go context.
func GetContextVars(ctx context.Context) *ContextVars {
	cv, _ := ctx.Value(contextVarsKey{}).(*ContextVars)
	return cv
}

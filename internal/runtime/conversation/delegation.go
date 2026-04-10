package conversation

import (
	"context"
	"fmt"
)

const (
	// DefaultMaxDelegationDepth is the maximum depth for recursive agent spawning.
	DefaultMaxDelegationDepth = 3
)

// DelegationConfig controls agent recursive delegation.
type DelegationConfig struct {
	MaxDepth     int
	AllowedTypes []string // agent types allowed to delegate (empty = all)
}

// DefaultDelegationConfig returns the default delegation config.
func DefaultDelegationConfig() *DelegationConfig {
	return &DelegationConfig{
		MaxDepth: DefaultMaxDelegationDepth,
	}
}

// DelegationContext tracks the current delegation depth and lineage.
type DelegationContext struct {
	Depth   int
	Lineage []string // agent IDs in the chain
	Config  *DelegationConfig
}

// NewDelegationContext creates a root delegation context.
func NewDelegationContext(cfg *DelegationConfig) *DelegationContext {
	if cfg == nil {
		cfg = DefaultDelegationConfig()
	}
	return &DelegationContext{
		Depth:  0,
		Config: cfg,
	}
}

// CanDelegate returns whether the current context allows spawning a child agent.
func (dc *DelegationContext) CanDelegate() bool {
	return dc.Depth < dc.Config.MaxDepth
}

// Child creates a new delegation context for a child agent.
func (dc *DelegationContext) Child(agentID string) (*DelegationContext, error) {
	if !dc.CanDelegate() {
		return nil, fmt.Errorf("maximum delegation depth (%d) reached", dc.Config.MaxDepth)
	}

	lineage := make([]string, len(dc.Lineage)+1)
	copy(lineage, dc.Lineage)
	lineage[len(dc.Lineage)] = agentID

	return &DelegationContext{
		Depth:   dc.Depth + 1,
		Lineage: lineage,
		Config:  dc.Config,
	}, nil
}

// RemainingDepth returns how many more levels of delegation are allowed.
func (dc *DelegationContext) RemainingDepth() int {
	remaining := dc.Config.MaxDepth - dc.Depth
	if remaining < 0 {
		return 0
	}
	return remaining
}

// delegationKey is the context key for delegation context.
type delegationKey struct{}

// WithDelegation adds delegation context to a context.
func WithDelegation(ctx context.Context, dc *DelegationContext) context.Context {
	return context.WithValue(ctx, delegationKey{}, dc)
}

// GetDelegation retrieves delegation context from a context.
func GetDelegation(ctx context.Context) *DelegationContext {
	dc, _ := ctx.Value(delegationKey{}).(*DelegationContext)
	return dc
}

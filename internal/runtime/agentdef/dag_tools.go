package agentdef

import "context"

// NodeToolRestrictions defines per-node tool access control in DAG workflows.
type NodeToolRestrictions struct {
	AllowedTools []string // if set, only these tools are available
	DeniedTools  []string // these tools are hidden even if in allowlist
}

type dagToolsCtxKey struct{}

// ContextWithNodeRestrictions returns a context carrying tool restrictions for a DAG node.
func ContextWithNodeRestrictions(ctx context.Context, r NodeToolRestrictions) context.Context {
	return context.WithValue(ctx, dagToolsCtxKey{}, &r)
}

// NodeRestrictionsFromContext retrieves tool restrictions from context, if present.
func NodeRestrictionsFromContext(ctx context.Context) *NodeToolRestrictions {
	v, _ := ctx.Value(dagToolsCtxKey{}).(*NodeToolRestrictions)
	return v
}

// HasRestrictions returns true if either AllowedTools or DeniedTools is non-empty.
func (r *NodeToolRestrictions) HasRestrictions() bool {
	return r != nil && (len(r.AllowedTools) > 0 || len(r.DeniedTools) > 0)
}

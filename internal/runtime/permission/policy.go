package permission

// Decision is the result of a permission check.
type Decision int

const (
	Allow Decision = iota
	Deny
	Ask
)

// String returns the string representation.
func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case Ask:
		return "ask"
	default:
		return "unknown"
	}
}

// Rule matches a tool name and specifies a decision.
type Rule struct {
	Tool     string   `json:"tool"`           // tool name or glob pattern
	Decision Decision `json:"decision"`       // allow, deny, ask
	Args     []string `json:"args,omitempty"` // optional arg patterns
}

// Policy holds a set of permission rules.
type Policy struct {
	Mode  Mode   `json:"mode"`
	Rules []Rule `json:"rules"`
}

// NewPolicy creates a new policy with the given mode.
func NewPolicy(mode Mode) *Policy {
	return &Policy{Mode: mode}
}

// AddRule adds a rule to the policy.
func (p *Policy) AddRule(r Rule) {
	p.Rules = append(p.Rules, r)
}

// Check evaluates whether a tool invocation is permitted.
// It checks explicit rules first, then falls back to mode-based checks.
func (p *Policy) Check(toolName string, requiredMode Mode) Decision {
	// Check explicit rules first.
	for _, r := range p.Rules {
		if matchTool(r.Tool, toolName) {
			return r.Decision
		}
	}

	// Fall back to mode-based check.
	if p.Mode.Allows(requiredMode) {
		return Allow
	}
	return Ask
}

// matchTool does a simple tool name match (exact or wildcard).
func matchTool(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	return pattern == name
}

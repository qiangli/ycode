package permission

import (
	"context"
	"fmt"
)

// Prompter asks the user for permission when a tool requires it.
type Prompter interface {
	// Prompt asks the user to allow or deny a tool invocation.
	// Returns true if the user allows it.
	Prompt(ctx context.Context, toolName string, description string) (bool, error)
}

// Enforcer checks and enforces tool permissions.
type Enforcer struct {
	policy   *Policy
	prompter Prompter
}

// NewEnforcer creates a permission enforcer.
func NewEnforcer(policy *Policy, prompter Prompter) *Enforcer {
	return &Enforcer{policy: policy, prompter: prompter}
}

// CheckAndEnforce checks permission and prompts the user if needed.
// Returns nil if allowed, error if denied.
func (e *Enforcer) CheckAndEnforce(ctx context.Context, toolName string, requiredMode Mode, description string) error {
	decision := e.policy.Check(toolName, requiredMode)

	switch decision {
	case Allow:
		return nil
	case Deny:
		return fmt.Errorf("permission denied for tool %q (mode: %s)", toolName, e.policy.Mode)
	case Ask:
		if e.prompter == nil {
			return fmt.Errorf("permission required for tool %q but no prompter available", toolName)
		}
		allowed, err := e.prompter.Prompt(ctx, toolName, description)
		if err != nil {
			return fmt.Errorf("prompt for permission: %w", err)
		}
		if !allowed {
			return fmt.Errorf("user denied permission for tool %q", toolName)
		}
		return nil
	default:
		return fmt.Errorf("unknown permission decision: %d", decision)
	}
}

// Policy returns the current policy.
func (e *Enforcer) Policy() *Policy {
	return e.policy
}

// SetPolicy replaces the policy.
func (e *Enforcer) SetPolicy(p *Policy) {
	e.policy = p
}

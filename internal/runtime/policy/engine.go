package policy

import (
	"fmt"
	"strings"
	"sync"
)

// Decision represents the outcome of a policy evaluation.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
	DecisionAsk   Decision = "ask"
)

// LaneEvent is a policy lane event for tracking decisions.
type LaneEvent struct {
	Tool       string   `json:"tool"`
	Decision   Decision `json:"decision"`
	Reason     string   `json:"reason,omitempty"`
	AppliedBy  string   `json:"applied_by,omitempty"` // rule name
}

// Rule defines a single policy rule.
type Rule struct {
	Name       string   `json:"name"`
	Tools      []string `json:"tools"`       // tool name patterns (* for wildcard)
	Paths      []string `json:"paths"`       // path patterns (optional)
	Decision   Decision `json:"decision"`
	Priority   int      `json:"priority"`    // higher = evaluated first
}

// Engine evaluates policy rules to determine tool access.
type Engine struct {
	mu     sync.RWMutex
	rules  []Rule
	events []LaneEvent
}

// NewEngine creates a new policy engine.
func NewEngine() *Engine {
	return &Engine{}
}

// AddRule adds a policy rule.
func (e *Engine) AddRule(rule Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, rule)
}

// Evaluate checks all rules for a tool invocation and returns the decision.
func (e *Engine) Evaluate(tool string, path string) (Decision, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Find highest-priority matching rule.
	var bestRule *Rule
	for i := range e.rules {
		rule := &e.rules[i]
		if !matchTool(rule.Tools, tool) {
			continue
		}
		if len(rule.Paths) > 0 && !matchPath(rule.Paths, path) {
			continue
		}
		if bestRule == nil || rule.Priority > bestRule.Priority {
			bestRule = rule
		}
	}

	if bestRule == nil {
		return DecisionAsk, "no matching policy rule"
	}

	event := LaneEvent{
		Tool:      tool,
		Decision:  bestRule.Decision,
		Reason:    fmt.Sprintf("matched rule %q", bestRule.Name),
		AppliedBy: bestRule.Name,
	}
	e.mu.RUnlock()
	e.mu.Lock()
	e.events = append(e.events, event)
	e.mu.Unlock()
	e.mu.RLock()

	return bestRule.Decision, event.Reason
}

// Events returns all lane events.
func (e *Engine) Events() []LaneEvent {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]LaneEvent{}, e.events...)
}

// matchTool checks if a tool name matches any pattern in the list.
func matchTool(patterns []string, tool string) bool {
	for _, p := range patterns {
		if p == "*" || p == tool {
			return true
		}
		if strings.HasSuffix(p, "*") && strings.HasPrefix(tool, p[:len(p)-1]) {
			return true
		}
	}
	return false
}

// matchPath checks if a path matches any pattern in the list.
func matchPath(patterns []string, path string) bool {
	for _, p := range patterns {
		if p == "*" || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

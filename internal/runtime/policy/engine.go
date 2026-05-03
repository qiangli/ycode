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
	Tool      string   `json:"tool"`
	Decision  Decision `json:"decision"`
	Reason    string   `json:"reason,omitempty"`
	AppliedBy string   `json:"applied_by,omitempty"` // rule name
}

// Rule defines a single policy rule.
type Rule struct {
	Name            string   `json:"name"`
	Tools           []string `json:"tools"`                      // tool name patterns (* for wildcard)
	Paths           []string `json:"paths"`                      // path patterns (optional)
	CommandPatterns []string `json:"command_patterns,omitempty"` // command-level patterns (e.g., "git *" for bash)
	Decision        Decision `json:"decision"`
	Priority        int      `json:"priority"` // higher = evaluated first
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
// The commandDetail parameter enables command-level pattern matching within a tool
// (e.g., matching "git commit" against a "git *" command pattern for bash).
func (e *Engine) Evaluate(tool string, commandDetail string) (Decision, string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Find highest-priority matching rule.
	var bestRule *Rule
	for i := range e.rules {
		rule := &e.rules[i]
		if !matchTool(rule.Tools, tool) {
			continue
		}
		if len(rule.Paths) > 0 && !MatchPattern(rule.Paths, commandDetail) {
			continue
		}
		// Check command patterns if present and detail provided.
		if len(rule.CommandPatterns) > 0 && commandDetail != "" {
			if !MatchPattern(rule.CommandPatterns, commandDetail) {
				continue
			}
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
	e.events = append(e.events, event)

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

// MatchPattern checks if a string matches any pattern in the list.
// Supports exact match, prefix wildcard ("git *"), and prefix match.
func MatchPattern(patterns []string, value string) bool {
	for _, p := range patterns {
		if p == "*" || p == value {
			return true
		}
		if strings.HasSuffix(p, "*") && strings.HasPrefix(value, p[:len(p)-1]) {
			return true
		}
		if strings.HasPrefix(value, p) {
			return true
		}
	}
	return false
}

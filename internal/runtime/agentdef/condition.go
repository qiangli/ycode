package agentdef

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Condition evaluates whether a flow step or DAG node should execute.
type Condition interface {
	Evaluate(ctx context.Context, vars map[string]string) (bool, error)
}

// ConditionConfig is the YAML-serializable condition definition.
type ConditionConfig struct {
	Type     string            `yaml:"type" json:"type"`                                 // output_contains, output_matches, score_above, all_of, any_of
	Value    string            `yaml:"value,omitempty" json:"value,omitempty"`           // comparison value
	Source   string            `yaml:"source,omitempty" json:"source,omitempty"`         // which node's output to check
	Children []ConditionConfig `yaml:"conditions,omitempty" json:"conditions,omitempty"` // for all_of/any_of
}

// RouteConfig defines conditional routing for a flow step.
type RouteConfig struct {
	When    ConditionConfig `yaml:"when" json:"when"`
	Target  string          `yaml:"then" json:"then"`                           // agent/node to route to
	Default string          `yaml:"default,omitempty" json:"default,omitempty"` // fallback
}

// Build converts a ConditionConfig into a Condition.
func (cc *ConditionConfig) Build() (Condition, error) {
	switch cc.Type {
	case "output_contains":
		return &containsCondition{source: cc.Source, value: cc.Value}, nil
	case "output_matches":
		re, err := regexp.Compile(cc.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", cc.Value, err)
		}
		return &matchesCondition{source: cc.Source, pattern: re}, nil
	case "score_above":
		threshold, err := strconv.ParseFloat(cc.Value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid threshold %q: %w", cc.Value, err)
		}
		return &scoreAboveCondition{source: cc.Source, threshold: threshold}, nil
	case "all_of":
		var children []Condition
		for i := range cc.Children {
			c, err := cc.Children[i].Build()
			if err != nil {
				return nil, fmt.Errorf("all_of[%d]: %w", i, err)
			}
			children = append(children, c)
		}
		return &allOfCondition{children: children}, nil
	case "any_of":
		var children []Condition
		for i := range cc.Children {
			c, err := cc.Children[i].Build()
			if err != nil {
				return nil, fmt.Errorf("any_of[%d]: %w", i, err)
			}
			children = append(children, c)
		}
		return &anyOfCondition{children: children}, nil
	default:
		return nil, fmt.Errorf("unknown condition type: %q", cc.Type)
	}
}

// containsCondition checks if a node's output contains a substring.
type containsCondition struct {
	source string
	value  string
}

func (c *containsCondition) Evaluate(_ context.Context, vars map[string]string) (bool, error) {
	output, ok := vars[c.source]
	if !ok {
		return false, nil
	}
	return strings.Contains(strings.ToLower(output), strings.ToLower(c.value)), nil
}

// matchesCondition checks if a node's output matches a regex.
type matchesCondition struct {
	source  string
	pattern *regexp.Regexp
}

func (c *matchesCondition) Evaluate(_ context.Context, vars map[string]string) (bool, error) {
	output, ok := vars[c.source]
	if !ok {
		return false, nil
	}
	return c.pattern.MatchString(output), nil
}

// scoreAboveCondition parses a numeric output and checks if it exceeds a threshold.
type scoreAboveCondition struct {
	source    string
	threshold float64
}

func (c *scoreAboveCondition) Evaluate(_ context.Context, vars map[string]string) (bool, error) {
	output, ok := vars[c.source]
	if !ok {
		return false, nil
	}
	score, err := strconv.ParseFloat(strings.TrimSpace(output), 64)
	if err != nil {
		return false, nil // non-numeric output: condition false
	}
	return score > c.threshold, nil
}

// allOfCondition requires all child conditions to pass (AND combinator).
type allOfCondition struct {
	children []Condition
}

func (c *allOfCondition) Evaluate(ctx context.Context, vars map[string]string) (bool, error) {
	for _, child := range c.children {
		ok, err := child.Evaluate(ctx, vars)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// anyOfCondition requires at least one child condition to pass (OR combinator).
type anyOfCondition struct {
	children []Condition
}

func (c *anyOfCondition) Evaluate(ctx context.Context, vars map[string]string) (bool, error) {
	for _, child := range c.children {
		ok, err := child.Evaluate(ctx, vars)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

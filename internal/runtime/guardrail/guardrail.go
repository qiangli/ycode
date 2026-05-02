// Package guardrail provides content-level validation for agent outputs.
// Guardrails complement the permission system: permissions gate what tools
// can be called; guardrails gate what outputs are acceptable.
//
// A Chain runs guardrails in order and retries with feedback on failure.
package guardrail

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Action determines what happens after a guardrail check.
type Action string

const (
	ActionAccept Action = "accept" // output is acceptable
	ActionRetry  Action = "retry"  // retry with feedback injected into prompt
	ActionReject Action = "reject" // fail the task immediately
)

// Result describes the outcome of a guardrail check.
type Result struct {
	Passed   bool   `json:"passed"`
	Feedback string `json:"feedback,omitempty"` // injected into retry prompt
	Action   Action `json:"action"`
}

// Guardrail validates agent output and decides whether to accept, retry, or reject.
type Guardrail interface {
	Name() string
	Check(ctx context.Context, input, output string) (*Result, error)
}

// Config is the YAML-serializable guardrail definition.
type Config struct {
	Type     string `yaml:"type" json:"type"`                             // schema, regex, command
	Criteria string `yaml:"criteria,omitempty" json:"criteria,omitempty"` // for LLM guardrails
	Pattern  string `yaml:"pattern,omitempty" json:"pattern,omitempty"`   // for regex guardrails
	Command  string `yaml:"command,omitempty" json:"command,omitempty"`   // for command guardrails
	Action   Action `yaml:"action,omitempty" json:"action,omitempty"`     // override default action (retry)
}

// EffectiveAction returns the action, defaulting to retry.
func (c *Config) EffectiveAction() Action {
	if c.Action == "" {
		return ActionRetry
	}
	return c.Action
}

// Chain runs multiple guardrails in order, short-circuiting on the first failure.
type Chain struct {
	guards   []Guardrail
	maxRetry int
}

// NewChain creates a guardrail chain.
func NewChain(guards []Guardrail, maxRetry int) *Chain {
	if maxRetry <= 0 {
		maxRetry = 2
	}
	return &Chain{guards: guards, maxRetry: maxRetry}
}

// MaxRetry returns the maximum number of retries.
func (c *Chain) MaxRetry() int { return c.maxRetry }

// Run executes all guardrails against the output.
// Returns nil result if all passed, or the first failure result.
func (c *Chain) Run(ctx context.Context, input, output string) (*Result, error) {
	for _, g := range c.guards {
		result, err := g.Check(ctx, input, output)
		if err != nil {
			return nil, fmt.Errorf("guardrail %q: %w", g.Name(), err)
		}
		if !result.Passed {
			return result, nil
		}
	}
	return &Result{Passed: true, Action: ActionAccept}, nil
}

// RegexGuardrail checks output for patterns that indicate unsafe content.
type RegexGuardrail struct {
	name    string
	pattern *regexp.Regexp
	action  Action
}

// NewRegexGuardrail creates a regex-based guardrail.
func NewRegexGuardrail(name, pattern string, action Action) (*RegexGuardrail, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}
	if action == "" {
		action = ActionReject
	}
	return &RegexGuardrail{name: name, pattern: re, action: action}, nil
}

func (g *RegexGuardrail) Name() string { return g.name }

func (g *RegexGuardrail) Check(_ context.Context, _, output string) (*Result, error) {
	if g.pattern.MatchString(output) {
		matches := g.pattern.FindAllString(output, 3)
		return &Result{
			Passed:   false,
			Action:   g.action,
			Feedback: fmt.Sprintf("Output contains prohibited pattern %q (matched: %s). Remove these and try again.", g.pattern.String(), strings.Join(matches, ", ")),
		}, nil
	}
	return &Result{Passed: true, Action: ActionAccept}, nil
}

// SchemaGuardrail validates that output conforms to a JSON schema.
type SchemaGuardrail struct {
	validator schemaValidator
}

// schemaValidator is an interface for schema validation, avoiding import cycles.
type schemaValidator interface {
	ValidateOutput(output string) (valid bool, errors []string)
}

// NewSchemaGuardrail creates a schema-based guardrail.
func NewSchemaGuardrail(v schemaValidator) *SchemaGuardrail {
	return &SchemaGuardrail{validator: v}
}

func (g *SchemaGuardrail) Name() string { return "schema" }

func (g *SchemaGuardrail) Check(_ context.Context, _, output string) (*Result, error) {
	valid, errs := g.validator.ValidateOutput(output)
	if !valid {
		return &Result{
			Passed:   false,
			Action:   ActionRetry,
			Feedback: fmt.Sprintf("Output does not match required schema: %s", strings.Join(errs, "; ")),
		}, nil
	}
	return &Result{Passed: true, Action: ActionAccept}, nil
}

// BuildFromConfigs creates a guardrail chain from YAML configs.
func BuildFromConfigs(configs []Config) (*Chain, error) {
	if len(configs) == 0 {
		return nil, nil
	}

	var guards []Guardrail
	for _, cfg := range configs {
		switch cfg.Type {
		case "regex":
			if cfg.Pattern == "" {
				return nil, fmt.Errorf("regex guardrail requires pattern")
			}
			g, err := NewRegexGuardrail("regex:"+cfg.Pattern, cfg.Pattern, cfg.EffectiveAction())
			if err != nil {
				return nil, err
			}
			guards = append(guards, g)
		default:
			return nil, fmt.Errorf("unknown guardrail type: %q", cfg.Type)
		}
	}

	return NewChain(guards, 2), nil
}

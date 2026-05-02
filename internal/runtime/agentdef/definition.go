// Package agentdef provides user-defined agent definitions loaded from YAML config files.
// Agent definitions specify custom system prompts, tool allowlists, model overrides,
// inheritance (embed), execution flow types, and AOP-style advices (before/around/after).
//
// Design aligned with github.com/qiangli/ai swarm config schema, adapted to Go conventions
// and ycode's RuntimeContext architecture.
package agentdef

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// APIVersion is the current schema version for agent definitions.
const APIVersion = "v1"

// AgentDefinition describes a user-defined agent loaded from YAML config.
type AgentDefinition struct {
	APIVersion  string            `yaml:"apiVersion" json:"apiVersion"`
	Name        string            `yaml:"name" json:"name"`
	Display     string            `yaml:"display,omitempty" json:"display,omitempty"`
	Description string            `yaml:"description" json:"description"`
	Instruction string            `yaml:"instruction" json:"instruction"`
	Context     string            `yaml:"context,omitempty" json:"context,omitempty"`
	Message     string            `yaml:"message,omitempty" json:"message,omitempty"`
	Mode        string            `yaml:"mode,omitempty" json:"mode,omitempty"`
	Model       string            `yaml:"model,omitempty" json:"model,omitempty"`
	Tools       []string          `yaml:"tools,omitempty" json:"tools,omitempty"`
	Embed       []string          `yaml:"embed,omitempty" json:"embed,omitempty"`
	Entrypoint  []string          `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Flow        FlowType          `yaml:"flow,omitempty" json:"flow,omitempty"`
	Advices     *AdvicesConfig    `yaml:"advices,omitempty" json:"advices,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	Arguments   map[string]any    `yaml:"arguments,omitempty" json:"arguments,omitempty"`
	MaxIter     int               `yaml:"max_iterations,omitempty" json:"max_iterations,omitempty"`
	MaxTime     int               `yaml:"max_time,omitempty" json:"max_time,omitempty"`
	Triggers    []TriggerPattern  `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Parameters  json.RawMessage   `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Nodes       []DAGNode         `yaml:"nodes,omitempty" json:"nodes,omitempty"`
	Process     string            `yaml:"process,omitempty" json:"process,omitempty"`

	// Structured output schema for validating agent responses.
	OutputSchema *OutputSchema `yaml:"output_schema,omitempty" json:"output_schema,omitempty"`

	// Guardrails for content-level validation of agent outputs.
	Guardrails []GuardrailConfig `yaml:"guardrails,omitempty" json:"guardrails,omitempty"`

	// A2AEndpoint is the URL of a remote A2A agent for delegation.
	A2AEndpoint string `yaml:"a2a_endpoint,omitempty" json:"a2a_endpoint,omitempty"`

	// A2AAuth holds authentication config for remote A2A connections.
	A2AAuth *A2AAuthConfig `yaml:"a2a_auth,omitempty" json:"a2a_auth,omitempty"`

	// Routes for conditional routing (used with FlowRouter).
	Routes []RouteConfig `yaml:"routes,omitempty" json:"routes,omitempty"`
}

// AdvicesConfig defines AOP-style hooks around agent execution.
type AdvicesConfig struct {
	Before []string `yaml:"before,omitempty" json:"before,omitempty"`
	Around []string `yaml:"around,omitempty" json:"around,omitempty"`
	After  []string `yaml:"after,omitempty" json:"after,omitempty"`
}

// TriggerPattern defines a keyword-trigger that auto-activates an agent.
type TriggerPattern struct {
	Pattern    string `yaml:"pattern" json:"pattern"`
	MaxPerTurn int    `yaml:"max_per_turn,omitempty" json:"max_per_turn,omitempty"`

	compiled *regexp.Regexp
}

// Compile compiles the regex pattern. Returns an error if the pattern is invalid.
func (tp *TriggerPattern) Compile() error {
	re, err := regexp.Compile(tp.Pattern)
	if err != nil {
		return fmt.Errorf("invalid trigger pattern %q: %w", tp.Pattern, err)
	}
	tp.compiled = re
	return nil
}

// Match returns true if the text matches the trigger pattern.
func (tp *TriggerPattern) Match(text string) bool {
	if tp.compiled == nil {
		return false
	}
	return tp.compiled.MatchString(text)
}

// FlowType defines how multiple actions (agents/tools) in an entrypoint are composed.
type FlowType string

const (
	FlowSequence FlowType = "sequence" // A then B then C, each uses previous output
	FlowChain    FlowType = "chain"    // A(B(C(...))) nested calls
	FlowParallel FlowType = "parallel" // A, B, C concurrent, combined results
	FlowLoop     FlowType = "loop"     // repeat until condition
	FlowFallback FlowType = "fallback" // try A, if fails try B, if fails try C
	FlowChoice   FlowType = "choice"   // random selection
	FlowDAG      FlowType = "dag"      // directed acyclic graph workflow
	FlowRouter   FlowType = "router"   // conditional routing based on output
)

// ValidFlowTypes is the set of recognized flow types.
var ValidFlowTypes = map[FlowType]bool{
	FlowSequence: true,
	FlowChain:    true,
	FlowParallel: true,
	FlowLoop:     true,
	FlowFallback: true,
	FlowChoice:   true,
	FlowDAG:      true,
	FlowRouter:   true,
}

// GuardrailConfig is the YAML-serializable guardrail definition embedded in AgentDefinition.
type GuardrailConfig struct {
	Type     string `yaml:"type" json:"type"`                             // schema, regex, command
	Criteria string `yaml:"criteria,omitempty" json:"criteria,omitempty"` // for LLM guardrails
	Pattern  string `yaml:"pattern,omitempty" json:"pattern,omitempty"`   // for regex guardrails
	Command  string `yaml:"command,omitempty" json:"command,omitempty"`   // for command guardrails
	Action   string `yaml:"action,omitempty" json:"action,omitempty"`     // accept, retry, reject
}

// A2AAuthConfig holds authentication configuration for remote A2A connections.
type A2AAuthConfig struct {
	Type   string `yaml:"type,omitempty" json:"type,omitempty"` // bearer, api_key
	Token  string `yaml:"token,omitempty" json:"token,omitempty"`
	Header string `yaml:"header,omitempty" json:"header,omitempty"` // custom header name
}

// nameRe validates agent names: lowercase alphanumeric plus underscore and hyphen.
var nameRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// validModes are the recognized agent modes.
var validModes = map[string]bool{
	"":        true, // default (build)
	"build":   true,
	"plan":    true,
	"explore": true,
}

// Validate checks the definition for structural correctness.
func (d *AgentDefinition) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("agent definition: name is required")
	}
	if !nameRe.MatchString(d.Name) {
		return fmt.Errorf("agent definition %q: name must match %s", d.Name, nameRe.String())
	}
	if d.Instruction == "" && len(d.Embed) == 0 {
		return fmt.Errorf("agent definition %q: instruction is required (unless embed is used)", d.Name)
	}
	if !validModes[d.Mode] {
		return fmt.Errorf("agent definition %q: invalid mode %q (valid: build, plan, explore)", d.Name, d.Mode)
	}
	if d.Flow != "" && !ValidFlowTypes[d.Flow] {
		return fmt.Errorf("agent definition %q: invalid flow type %q", d.Name, d.Flow)
	}
	if d.MaxIter < 0 {
		return fmt.Errorf("agent definition %q: max_iterations must be >= 0", d.Name)
	}
	if d.MaxTime < 0 {
		return fmt.Errorf("agent definition %q: max_time must be >= 0", d.Name)
	}
	for i := range d.Triggers {
		if err := d.Triggers[i].Compile(); err != nil {
			return fmt.Errorf("agent definition %q: trigger %d: %w", d.Name, i, err)
		}
	}
	return nil
}

// EffectiveMode returns the agent mode, defaulting to "build".
func (d *AgentDefinition) EffectiveMode() string {
	if d.Mode == "" {
		return "build"
	}
	return d.Mode
}

// EffectiveMaxIter returns the max iterations, defaulting to 15.
func (d *AgentDefinition) EffectiveMaxIter() int {
	if d.MaxIter <= 0 {
		return 15
	}
	return d.MaxIter
}

// EffectiveTimeout returns the timeout duration, defaulting to 0 (no timeout).
func (d *AgentDefinition) EffectiveTimeout() time.Duration {
	if d.MaxTime <= 0 {
		return 0
	}
	return time.Duration(d.MaxTime) * time.Second
}

// MergeFrom merges fields from an ancestor definition (embed).
// Only empty fields in d are filled from ancestor. Tools and Environment are unioned.
func (d *AgentDefinition) MergeFrom(ancestor *AgentDefinition) {
	if d.Instruction == "" {
		d.Instruction = ancestor.Instruction
	}
	if d.Context == "" {
		d.Context = ancestor.Context
	}
	if d.Mode == "" {
		d.Mode = ancestor.Mode
	}
	if d.Model == "" {
		d.Model = ancestor.Model
	}
	if d.MaxIter == 0 {
		d.MaxIter = ancestor.MaxIter
	}
	if d.MaxTime == 0 {
		d.MaxTime = ancestor.MaxTime
	}
	if d.Flow == "" {
		d.Flow = ancestor.Flow
	}

	// Union tools.
	if len(ancestor.Tools) > 0 {
		seen := make(map[string]bool, len(d.Tools))
		for _, t := range d.Tools {
			seen[t] = true
		}
		for _, t := range ancestor.Tools {
			if !seen[t] {
				d.Tools = append(d.Tools, t)
			}
		}
	}

	// Union environment.
	if len(ancestor.Environment) > 0 {
		if d.Environment == nil {
			d.Environment = make(map[string]string)
		}
		for k, v := range ancestor.Environment {
			if _, exists := d.Environment[k]; !exists {
				d.Environment[k] = v
			}
		}
	}

	// Inherit entrypoint if not set.
	if len(d.Entrypoint) == 0 {
		d.Entrypoint = ancestor.Entrypoint
	}

	// Inherit advices if not set.
	if d.Advices == nil && ancestor.Advices != nil {
		copied := *ancestor.Advices
		d.Advices = &copied
	}
}

// IsCustom returns true if this definition was loaded from config (not a hardcoded type).
func (d *AgentDefinition) IsCustom() bool {
	return d.APIVersion != ""
}

// DisplayName returns the display name, falling back to the name.
func (d *AgentDefinition) DisplayName() string {
	if d.Display != "" {
		return d.Display
	}
	return d.Name
}

// MatchesTrigger checks if any of the definition's triggers match the given text.
func (d *AgentDefinition) MatchesTrigger(text string) bool {
	text = strings.ToLower(text)
	for _, t := range d.Triggers {
		if t.Match(text) {
			return true
		}
	}
	return false
}

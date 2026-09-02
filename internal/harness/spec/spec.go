// Package spec defines and compiles the declarative ycode harness document.
package spec

import "encoding/json"

const (
	APIVersion = "ycode.dev/v1alpha1"
	Kind       = "Harness"
)

// Document is the complete configuration of a ycode process. There are no
// behavioral defaults: a runnable document names every resource and policy it
// uses.
type Document struct {
	APIVersion    string              `yaml:"apiVersion" json:"apiVersion"`
	Kind          string              `yaml:"kind" json:"kind"`
	Metadata      Metadata            `yaml:"metadata" json:"metadata"`
	Harness       Harness             `yaml:"harness" json:"harness"`
	Providers     map[string]Provider `yaml:"providers" json:"providers"`
	Models        map[string]Model    `yaml:"models" json:"models"`
	Context       map[string]Context  `yaml:"context" json:"context"`
	Input         Input               `yaml:"input" json:"input"`
	Memory        map[string]Memory   `yaml:"memory" json:"memory"`
	Bashy         Bashy               `yaml:"bashy" json:"bashy"`
	HITL          HITL                `yaml:"hitl" json:"hitl"`
	Sessions      Sessions            `yaml:"sessions" json:"sessions"`
	Pipelines     map[string]Pipeline `yaml:"pipelines" json:"pipelines"`
	Agents        map[string]Agent    `yaml:"agents" json:"agents"`
	Frontends     map[string]Frontend `yaml:"frontends" json:"frontends"`
	Observability Observability       `yaml:"observability" json:"observability"`
	Source        string              `yaml:"-" json:"-"`
	BaseDir       string              `yaml:"-" json:"-"`
}

type Metadata struct {
	Name    string `yaml:"name" json:"name"`
	Version int    `yaml:"version" json:"version"`
}

type Harness struct {
	DefaultAgent  string   `yaml:"defaultAgent" json:"defaultAgent"`
	Pipeline      string   `yaml:"pipeline" json:"pipeline"`
	Workspace     string   `yaml:"workspace" json:"workspace"`
	ReadableRoots []string `yaml:"readableRoots" json:"readableRoots"`
}

type Provider struct {
	Type    string `yaml:"type" json:"type"`
	BaseURL string `yaml:"baseURL" json:"baseURL"`
	APIKey  string `yaml:"apiKey" json:"-" secret:"true"`
}

type Model struct {
	Provider        string  `yaml:"provider" json:"provider"`
	Name            string  `yaml:"name" json:"name"`
	MaxOutputTokens int     `yaml:"maxOutputTokens" json:"maxOutputTokens"`
	Temperature     float64 `yaml:"temperature" json:"temperature"`
}

type ContentSource struct {
	Text     string `yaml:"text,omitempty" json:"text,omitempty"`
	File     string `yaml:"file,omitempty" json:"file,omitempty"`
	Required bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Resolved string `yaml:"-" json:"-"`
}

type Context struct {
	Sources  []ContentSource `yaml:"sources" json:"sources"`
	MaxBytes int             `yaml:"maxBytes" json:"maxBytes"`
}

type Input struct {
	Sources []string `yaml:"sources" json:"sources"`
	Queue   string   `yaml:"queue" json:"queue"`
}

type Memory struct {
	Provider   string           `yaml:"provider" json:"provider"`
	Recall     RecallPolicy     `yaml:"recall" json:"recall"`
	Write      WritePolicy      `yaml:"write" json:"write"`
	Compaction CompactionPolicy `yaml:"compaction" json:"compaction"`
}

type RecallPolicy struct {
	MaxItems  int `yaml:"maxItems" json:"maxItems"`
	MaxTokens int `yaml:"maxTokens" json:"maxTokens"`
}

type WritePolicy struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type CompactionPolicy struct {
	TriggerTokens        int `yaml:"triggerTokens" json:"triggerTokens"`
	PreserveRecentTokens int `yaml:"preserveRecentTokens" json:"preserveRecentTokens"`
}

type Bashy struct {
	ToolName       string `yaml:"toolName" json:"toolName"`
	Description    string `yaml:"description" json:"description"`
	TimeoutMS      int    `yaml:"timeoutMs" json:"timeoutMs"`
	MaxOutputChars int    `yaml:"maxOutputChars" json:"maxOutputChars"`
	MaxParallel    int    `yaml:"maxParallel" json:"maxParallel"`
	Permission     string `yaml:"permission" json:"permission"`
	Preflight      string `yaml:"preflight" json:"preflight"`
}

type HITL struct {
	OnUnavailable string     `yaml:"onUnavailable" json:"onUnavailable"`
	Decisions     []string   `yaml:"decisions" json:"decisions"`
	Rules         []HITLRule `yaml:"rules" json:"rules"`
}

type StringList []string

func (s *StringList) UnmarshalYAML(unmarshal func(any) error) error {
	var many []string
	if err := unmarshal(&many); err == nil {
		*s = many
		return nil
	}
	var one string
	if err := unmarshal(&one); err != nil {
		return err
	}
	*s = []string{one}
	return nil
}

type HITLRule struct {
	Effect StringList `yaml:"effect" json:"effect"`
	Action string     `yaml:"action" json:"action"`
}

type Sessions struct {
	Store                     string `yaml:"store" json:"store"`
	Directory                 string `yaml:"directory" json:"directory"`
	CheckpointBeforeInterrupt bool   `yaml:"checkpointBeforeInterrupt" json:"checkpointBeforeInterrupt"`
}

type Stage struct {
	ID       string         `yaml:"id,omitempty" json:"id,omitempty"`
	Needs    []string       `yaml:"needs,omitempty" json:"needs,omitempty"`
	Use      string         `yaml:"use,omitempty" json:"use,omitempty"`
	Call     string         `yaml:"call,omitempty" json:"call,omitempty"`
	With     map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
	When     string         `yaml:"when,omitempty" json:"when,omitempty"`
	Stages   []Stage        `yaml:"stages,omitempty" json:"stages,omitempty"`
	Repeat   *Repeat        `yaml:"repeat,omitempty" json:"repeat,omitempty"`
	Retry    *Retry         `yaml:"retry,omitempty" json:"retry,omitempty"`
	Switch   *Switch        `yaml:"switch,omitempty" json:"switch,omitempty"`
	ForEach  *ForEach       `yaml:"forEach,omitempty" json:"forEach,omitempty"`
	Fallback []Stage        `yaml:"fallback,omitempty" json:"fallback,omitempty"`
}

// Pipeline is a reusable dependency graph. Declaration order is only a stable
// tie-breaker; Needs controls execution and therefore all fan-in/fan-out.
type Pipeline struct {
	Inputs      []string `yaml:"inputs" json:"inputs"`
	Outputs     []string `yaml:"outputs" json:"outputs"`
	Concurrency int      `yaml:"concurrency" json:"concurrency"`
	FailFast    bool     `yaml:"failFast" json:"failFast"`
	Nodes       []Stage  `yaml:"nodes" json:"nodes"`
}

type Repeat struct {
	Max    int     `yaml:"max" json:"max"`
	Until  string  `yaml:"until" json:"until"`
	Stages []Stage `yaml:"stages" json:"stages"`
}

type Retry struct {
	Max    int     `yaml:"max" json:"max"`
	Stages []Stage `yaml:"stages" json:"stages"`
}

type Switch struct {
	Cases   []Case  `yaml:"cases" json:"cases"`
	Default []Stage `yaml:"default,omitempty" json:"default,omitempty"`
}

type Case struct {
	When   string  `yaml:"when" json:"when"`
	Stages []Stage `yaml:"stages" json:"stages"`
}

type ForEach struct {
	Items       string  `yaml:"items" json:"items"`
	As          string  `yaml:"as" json:"as"`
	Collect     string  `yaml:"collect" json:"collect"`
	MaxParallel int     `yaml:"maxParallel" json:"maxParallel"`
	Ordered     bool    `yaml:"ordered" json:"ordered"`
	Stages      []Stage `yaml:"stages" json:"stages"`
}

type Agent struct {
	Model    string          `yaml:"model" json:"model"`
	Pipeline string          `yaml:"pipeline" json:"pipeline"`
	Memory   string          `yaml:"memory" json:"memory"`
	System   []ContentSource `yaml:"system" json:"system"`
}

type Frontend struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Address string `yaml:"address,omitempty" json:"address,omitempty"`
}

type Observability struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	Exporter      string `yaml:"exporter" json:"exporter"`
	Endpoint      string `yaml:"endpoint" json:"endpoint"`
	RecordContent bool   `yaml:"recordContent" json:"recordContent"`
}

// RedactedJSON returns the operator-facing representation of a compiled
// document. Provider credentials are never part of this representation.
func (d *Document) RedactedJSON() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

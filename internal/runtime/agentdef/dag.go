package agentdef

import (
	"fmt"
	"sort"
	"strings"
)

// DAGNodeType identifies the kind of node in a DAG workflow.
type DAGNodeType string

const (
	NodeTypePrompt   DAGNodeType = "prompt"   // AI prompt
	NodeTypeBash     DAGNodeType = "bash"     // deterministic shell command
	NodeTypeApproval DAGNodeType = "approval" // human gate
	NodeTypeAgent    DAGNodeType = "agent"    // spawn a sub-agent
)

// DAGNode is a single node in a DAG workflow.
type DAGNode struct {
	ID              string            `yaml:"id" json:"id"`
	Type            DAGNodeType       `yaml:"type" json:"type"`
	Name            string            `yaml:"name,omitempty" json:"name,omitempty"`
	Prompt          string            `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Command         string            `yaml:"command,omitempty" json:"command,omitempty"`
	AgentName       string            `yaml:"agent,omitempty" json:"agent,omitempty"`
	DependsOn       []string          `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Interactive     bool              `yaml:"interactive,omitempty" json:"interactive,omitempty"`           // for approval nodes
	CaptureResponse bool              `yaml:"capture_response,omitempty" json:"capture_response,omitempty"` // for approval nodes
	Variables       map[string]string `yaml:"variables,omitempty" json:"variables,omitempty"`

	// When specifies a condition that must be true for this node to execute.
	// If the condition evaluates to false, the node is skipped.
	When *ConditionConfig `yaml:"when,omitempty" json:"when,omitempty"`

	// AllowedTools restricts tools available to this node (whitelist).
	AllowedTools []string `yaml:"allowed_tools,omitempty" json:"allowed_tools,omitempty"`
	// DeniedTools hides specific tools from this node (blacklist).
	DeniedTools []string `yaml:"denied_tools,omitempty" json:"denied_tools,omitempty"`

	// FanOut enables dynamic parallelism: the node is executed once per item
	// in the upstream output. The upstream node's output is split by SplitOn
	// (default: newline) and each slice is passed to a separate instance of
	// this node via the $fan.item and $fan.index template variables.
	// Results are collected and joined by JoinWith (default: newline).
	// Inspired by LangGraph's Send() pattern for map-reduce workflows.
	FanOut *FanOutConfig `yaml:"fan_out,omitempty" json:"fan_out,omitempty"`
}

// FanOutConfig controls dynamic parallel execution of a DAG node.
type FanOutConfig struct {
	// SourceNode is the upstream node whose output is split into items.
	SourceNode string `yaml:"source_node" json:"source_node"`
	// SplitOn is the delimiter used to split the source output into items.
	// Default: "\n" (newline).
	SplitOn string `yaml:"split_on,omitempty" json:"split_on,omitempty"`
	// JoinWith is the delimiter used to join fan-out results.
	// Default: "\n" (newline).
	JoinWith string `yaml:"join_with,omitempty" json:"join_with,omitempty"`
	// MaxParallel limits the number of concurrent fan-out instances.
	// Default: 0 (unlimited).
	MaxParallel int `yaml:"max_parallel,omitempty" json:"max_parallel,omitempty"`
}

// EffectiveSplitOn returns the split delimiter, defaulting to newline.
func (f *FanOutConfig) EffectiveSplitOn() string {
	if f.SplitOn == "" {
		return "\n"
	}
	return f.SplitOn
}

// EffectiveJoinWith returns the join delimiter, defaulting to newline.
func (f *FanOutConfig) EffectiveJoinWith() string {
	if f.JoinWith == "" {
		return "\n"
	}
	return f.JoinWith
}

// DAGWorkflow is a complete DAG definition.
type DAGWorkflow struct {
	Name        string    `yaml:"name" json:"name"`
	Description string    `yaml:"description,omitempty" json:"description,omitempty"`
	Nodes       []DAGNode `yaml:"nodes" json:"nodes"`
}

// TopologicalSort returns nodes grouped into layers for concurrent execution.
// Nodes within a layer have no dependencies on each other.
func TopologicalSort(nodes []DAGNode) ([][]DAGNode, error) {
	// Build adjacency and in-degree maps.
	nodeMap := make(map[string]*DAGNode)
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // node -> nodes that depend on it

	for i := range nodes {
		nodeMap[nodes[i].ID] = &nodes[i]
		inDegree[nodes[i].ID] = 0
	}

	for _, node := range nodes {
		for _, dep := range node.DependsOn {
			if _, ok := nodeMap[dep]; !ok {
				return nil, fmt.Errorf("node %q depends on unknown node %q", node.ID, dep)
			}
			inDegree[node.ID]++
			dependents[dep] = append(dependents[dep], node.ID)
		}
	}

	// Kahn's algorithm — group into layers.
	var layers [][]DAGNode
	remaining := len(nodes)

	for remaining > 0 {
		// Find all nodes with in-degree 0.
		var layer []DAGNode
		for _, node := range nodes {
			if inDegree[node.ID] == 0 {
				layer = append(layer, node)
			}
		}
		if len(layer) == 0 {
			return nil, fmt.Errorf("cycle detected in DAG")
		}

		// Sort layer by ID for deterministic order.
		sort.Slice(layer, func(i, j int) bool {
			return layer[i].ID < layer[j].ID
		})

		// Remove processed nodes.
		for _, node := range layer {
			inDegree[node.ID] = -1 // mark as processed
			for _, dep := range dependents[node.ID] {
				inDegree[dep]--
			}
		}

		layers = append(layers, layer)
		remaining -= len(layer)
	}

	return layers, nil
}

// SubstituteVariables replaces $nodeId.output references in text with actual outputs.
func SubstituteVariables(text string, outputs map[string]string) string {
	for id, output := range outputs {
		text = strings.ReplaceAll(text, "$"+id+".output", output)
	}
	return text
}

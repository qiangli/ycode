package agentdef

import (
	"context"
	"strings"
	"testing"
)

// TestDAGExecutor_ConditionalNodes tests that DAG nodes with When conditions
// are skipped when the condition evaluates to false.
func TestDAGExecutor_ConditionalNodes(t *testing.T) {
	handler := func(_ context.Context, node DAGNode, vars map[string]string) (string, error) {
		prompt := SubstituteVariables(node.Prompt, vars)
		return "output:" + node.ID + ":" + prompt, nil
	}

	workflow := &DAGWorkflow{
		Name: "conditional-test",
		Nodes: []DAGNode{
			{
				ID:     "analyze",
				Type:   NodeTypePrompt,
				Prompt: "analyze code",
			},
			{
				ID:        "fix",
				Type:      NodeTypeAgent,
				Prompt:    "fix issues from $analyze.output",
				DependsOn: []string{"analyze"},
				When: &ConditionConfig{
					Type:   "output_contains",
					Source: "analyze",
					Value:  "issues found",
				},
			},
			{
				ID:        "approve",
				Type:      NodeTypePrompt,
				Prompt:    "approve $analyze.output",
				DependsOn: []string{"analyze"},
				When: &ConditionConfig{
					Type:   "output_contains",
					Source: "analyze",
					Value:  "no issues",
				},
			},
		},
	}

	executor := NewDAGExecutor(handler)
	outputs, err := executor.Run(context.Background(), workflow)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// analyze output doesn't contain "issues found" or "no issues"
	// so both conditional nodes should be skipped.
	if _, exists := outputs["fix"]; exists {
		t.Fatal("fix should have been skipped (condition not met)")
	}
	if _, exists := outputs["approve"]; exists {
		t.Fatal("approve should have been skipped (condition not met)")
	}
	if _, exists := outputs["analyze"]; !exists {
		t.Fatal("analyze should have run")
	}
}

// TestDAGExecutor_ConditionalNodes_OneMatches tests that only the matching
// conditional node executes.
func TestDAGExecutor_ConditionalNodes_OneMatches(t *testing.T) {
	handler := func(_ context.Context, node DAGNode, vars map[string]string) (string, error) {
		if node.ID == "analyze" {
			return "3 issues found in the code", nil
		}
		prompt := SubstituteVariables(node.Prompt, vars)
		return "executed:" + node.ID + ":" + prompt, nil
	}

	workflow := &DAGWorkflow{
		Name: "conditional-match",
		Nodes: []DAGNode{
			{
				ID:     "analyze",
				Type:   NodeTypePrompt,
				Prompt: "analyze",
			},
			{
				ID:        "fix",
				Type:      NodeTypeAgent,
				Prompt:    "fix",
				DependsOn: []string{"analyze"},
				When: &ConditionConfig{
					Type:   "output_contains",
					Source: "analyze",
					Value:  "issues found",
				},
			},
			{
				ID:        "approve",
				Type:      NodeTypePrompt,
				Prompt:    "approve",
				DependsOn: []string{"analyze"},
				When: &ConditionConfig{
					Type:   "output_contains",
					Source: "analyze",
					Value:  "no issues",
				},
			},
		},
	}

	executor := NewDAGExecutor(handler)
	outputs, err := executor.Run(context.Background(), workflow)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// "fix" should run (analyze output contains "issues found").
	if _, exists := outputs["fix"]; !exists {
		t.Fatal("fix should have run")
	}
	// "approve" should be skipped (analyze output doesn't contain "no issues").
	if _, exists := outputs["approve"]; exists {
		t.Fatal("approve should have been skipped")
	}
}

// TestFlowRouter_ConditionRouting tests the router flow type with conditional routing.
func TestFlowRouter_ConditionRouting(t *testing.T) {
	// First action: produce output.
	inputAction := func(_ context.Context, input string) (string, error) {
		return "error detected in output", nil
	}

	fixAction := func(_ context.Context, input string) (string, error) {
		return "fixed: " + input, nil
	}

	approveAction := func(_ context.Context, input string) (string, error) {
		return "approved: " + input, nil
	}

	fe := NewFlowExecutor(FlowRouter, []Action{inputAction})
	fe.SetRoutes(
		[]RouteConfig{
			{
				When:   ConditionConfig{Type: "output_contains", Source: "input", Value: "error"},
				Target: "fixer",
			},
			{
				When:    ConditionConfig{Type: "output_contains", Source: "input", Value: "clean"},
				Target:  "approver",
				Default: "approver",
			},
		},
		map[string]Action{
			"fixer":    fixAction,
			"approver": approveAction,
		},
	)

	result, err := fe.Run(context.Background(), "test input")
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.HasPrefix(result, "fixed:") {
		t.Fatalf("expected fixed output, got: %s", result)
	}
}

// TestFlowRouter_DefaultRoute tests that the default route is used when no condition matches.
func TestFlowRouter_DefaultRoute(t *testing.T) {
	inputAction := func(_ context.Context, input string) (string, error) {
		return "something unrelated", nil
	}

	defaultAction := func(_ context.Context, input string) (string, error) {
		return "default: " + input, nil
	}

	fe := NewFlowExecutor(FlowRouter, []Action{inputAction})
	fe.SetRoutes(
		[]RouteConfig{
			{
				When:    ConditionConfig{Type: "output_contains", Source: "input", Value: "NEVER_MATCH_THIS"},
				Target:  "special",
				Default: "fallback",
			},
		},
		map[string]Action{
			"special":  func(_ context.Context, _ string) (string, error) { return "special", nil },
			"fallback": defaultAction,
		},
	)

	result, err := fe.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.HasPrefix(result, "default:") {
		t.Fatalf("expected default output, got: %s", result)
	}
}

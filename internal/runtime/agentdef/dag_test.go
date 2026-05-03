package agentdef

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTopologicalSort_Basic(t *testing.T) {
	nodes := []DAGNode{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}
	layers, err := TopologicalSort(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}
	if layers[0][0].ID != "a" {
		t.Errorf("layer 0: expected a, got %s", layers[0][0].ID)
	}
	if layers[1][0].ID != "b" {
		t.Errorf("layer 1: expected b, got %s", layers[1][0].ID)
	}
	if layers[2][0].ID != "c" {
		t.Errorf("layer 2: expected c, got %s", layers[2][0].ID)
	}
}

func TestTopologicalSort_ConcurrentLayers(t *testing.T) {
	nodes := []DAGNode{
		{ID: "a"},
		{ID: "b"},
		{ID: "c", DependsOn: []string{"a", "b"}},
	}
	layers, err := TopologicalSort(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(layers))
	}
	if len(layers[0]) != 2 {
		t.Fatalf("layer 0: expected 2 nodes, got %d", len(layers[0]))
	}
	// Sorted by ID.
	if layers[0][0].ID != "a" || layers[0][1].ID != "b" {
		t.Errorf("layer 0: expected [a, b], got [%s, %s]", layers[0][0].ID, layers[0][1].ID)
	}
	if layers[1][0].ID != "c" {
		t.Errorf("layer 1: expected c, got %s", layers[1][0].ID)
	}
}

func TestTopologicalSort_CycleDetected(t *testing.T) {
	nodes := []DAGNode{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}
	_, err := TopologicalSort(nodes)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle error, got: %v", err)
	}
}

func TestTopologicalSort_UnknownDependency(t *testing.T) {
	nodes := []DAGNode{
		{ID: "a", DependsOn: []string{"nonexistent"}},
	}
	_, err := TopologicalSort(nodes)
	if err == nil {
		t.Fatal("expected unknown dependency error")
	}
	if !strings.Contains(err.Error(), "unknown node") {
		t.Errorf("expected unknown node error, got: %v", err)
	}
}

func TestSubstituteVariables(t *testing.T) {
	outputs := map[string]string{
		"step1": "hello",
		"step2": "world",
	}
	text := "Result: $step1.output and $step2.output"
	result := SubstituteVariables(text, outputs)
	expected := "Result: hello and world"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSubstituteVariables_NoMatch(t *testing.T) {
	text := "no variables here"
	result := SubstituteVariables(text, map[string]string{"x": "y"})
	if result != text {
		t.Errorf("expected unchanged text, got %q", result)
	}
}

func TestDAGExecutor_Run(t *testing.T) {
	workflow := &DAGWorkflow{
		Name: "test",
		Nodes: []DAGNode{
			{ID: "a", Type: NodeTypePrompt, Prompt: "start"},
			{ID: "b", Type: NodeTypeBash, Command: "echo", DependsOn: []string{"a"}},
			{ID: "c", Type: NodeTypePrompt, Prompt: "finish $a.output", DependsOn: []string{"b"}},
		},
	}

	handler := func(_ context.Context, node DAGNode, vars map[string]string) (string, error) {
		switch node.ID {
		case "a":
			return "result-a", nil
		case "b":
			return "result-b", nil
		case "c":
			return SubstituteVariables(node.Prompt, vars), nil
		}
		return "", fmt.Errorf("unexpected node: %s", node.ID)
	}

	executor := NewDAGExecutor(handler)
	outputs, err := executor.Run(context.Background(), workflow)
	if err != nil {
		t.Fatal(err)
	}
	if outputs["a"] != "result-a" {
		t.Errorf("expected result-a, got %s", outputs["a"])
	}
	if outputs["b"] != "result-b" {
		t.Errorf("expected result-b, got %s", outputs["b"])
	}
	if outputs["c"] != "finish result-a" {
		t.Errorf("expected 'finish result-a', got %s", outputs["c"])
	}
}

func TestDAGExecutor_ConcurrentExecution(t *testing.T) {
	workflow := &DAGWorkflow{
		Name: "concurrent",
		Nodes: []DAGNode{
			{ID: "a"},
			{ID: "b"},
			{ID: "c", DependsOn: []string{"a", "b"}},
		},
	}

	var concurrentCalls atomic.Int32

	handler := func(_ context.Context, node DAGNode, _ map[string]string) (string, error) {
		concurrentCalls.Add(1)
		return node.ID + "-done", nil
	}

	executor := NewDAGExecutor(handler)
	outputs, err := executor.Run(context.Background(), workflow)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(outputs))
	}
	if outputs["c"] != "c-done" {
		t.Errorf("expected c-done, got %s", outputs["c"])
	}
}

func TestDAGExecutor_NodeError(t *testing.T) {
	workflow := &DAGWorkflow{
		Name: "error",
		Nodes: []DAGNode{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a"}},
		},
	}

	handler := func(_ context.Context, node DAGNode, _ map[string]string) (string, error) {
		if node.ID == "a" {
			return "", fmt.Errorf("failed")
		}
		return "ok", nil
	}

	executor := NewDAGExecutor(handler)
	_, err := executor.Run(context.Background(), workflow)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "node a") {
		t.Errorf("expected node a error, got: %v", err)
	}
}

func TestDAGExecutor_FanOut(t *testing.T) {
	workflow := &DAGWorkflow{
		Name: "fan-out-test",
		Nodes: []DAGNode{
			{ID: "source", Type: NodeTypePrompt, Prompt: "generate items"},
			{
				ID:        "process",
				Type:      NodeTypePrompt,
				Prompt:    "process $fan.item at index $fan.index",
				DependsOn: []string{"source"},
				FanOut: &FanOutConfig{
					SourceNode:  "source",
					MaxParallel: 2,
				},
			},
		},
	}

	var callCount atomic.Int32
	handler := func(_ context.Context, node DAGNode, vars map[string]string) (string, error) {
		callCount.Add(1)
		if node.ID == "source" {
			return "alpha\nbeta\ngamma", nil
		}
		// process node receives substituted prompt
		return "done:" + node.Prompt, nil
	}

	executor := NewDAGExecutor(handler)
	outputs, err := executor.Run(context.Background(), workflow)
	if err != nil {
		t.Fatal(err)
	}

	// source + 3 fan-out instances = 4 calls
	if got := callCount.Load(); got != 4 {
		t.Errorf("expected 4 handler calls, got %d", got)
	}

	// Fan-out results should be joined by newline.
	result := outputs["process"]
	if !strings.Contains(result, "done:") {
		t.Errorf("expected fan-out results, got: %s", result)
	}
	parts := strings.Split(result, "\n")
	if len(parts) != 3 {
		t.Errorf("expected 3 fan-out results, got %d: %v", len(parts), parts)
	}
}

func TestDAGExecutor_FanOut_EmptySource(t *testing.T) {
	workflow := &DAGWorkflow{
		Name: "fan-out-empty",
		Nodes: []DAGNode{
			{ID: "source", Type: NodeTypePrompt},
			{
				ID:        "process",
				Type:      NodeTypePrompt,
				Prompt:    "process $fan.item",
				DependsOn: []string{"source"},
				FanOut: &FanOutConfig{
					SourceNode: "source",
				},
			},
		},
	}

	handler := func(_ context.Context, node DAGNode, _ map[string]string) (string, error) {
		if node.ID == "source" {
			return "", nil // empty output
		}
		return "should-not-reach", nil
	}

	executor := NewDAGExecutor(handler)
	outputs, err := executor.Run(context.Background(), workflow)
	if err != nil {
		t.Fatal(err)
	}
	if outputs["process"] != "" {
		t.Errorf("expected empty result for empty fan-out, got: %s", outputs["process"])
	}
}

func TestFanOutConfig_Defaults(t *testing.T) {
	cfg := &FanOutConfig{SourceNode: "src"}
	if got := cfg.EffectiveSplitOn(); got != "\n" {
		t.Errorf("EffectiveSplitOn() = %q, want %q", got, "\n")
	}
	if got := cfg.EffectiveJoinWith(); got != "\n" {
		t.Errorf("EffectiveJoinWith() = %q, want %q", got, "\n")
	}

	cfg2 := &FanOutConfig{SourceNode: "src", SplitOn: ",", JoinWith: ";"}
	if got := cfg2.EffectiveSplitOn(); got != "," {
		t.Errorf("EffectiveSplitOn() = %q, want %q", got, ",")
	}
	if got := cfg2.EffectiveJoinWith(); got != ";" {
		t.Errorf("EffectiveJoinWith() = %q, want %q", got, ";")
	}
}

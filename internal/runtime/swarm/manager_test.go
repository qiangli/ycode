package swarm

import (
	"context"
	"strings"
	"testing"
)

func TestHierarchicalManager_Run(t *testing.T) {
	cfg := ManagerConfig{
		Agents: []AgentSpec{
			{Name: "coder", Role: "developer", Description: "Writes code"},
			{Name: "reviewer", Role: "reviewer", Description: "Reviews code"},
		},
	}

	mgr := NewHierarchicalManager(cfg)
	mgr.DecomposeFunc = func(_ context.Context, prompt string) (string, error) {
		return "1. coder writes code\n2. reviewer reviews it", nil
	}
	mgr.DelegateFunc = func(_ context.Context, agentName, task string) (string, error) {
		return "done by " + agentName, nil
	}

	result, err := mgr.Run(context.Background(), "build a feature")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "[coder] done by coder") {
		t.Errorf("expected coder result, got: %s", result)
	}
	if !strings.Contains(result, "[reviewer] done by reviewer") {
		t.Errorf("expected reviewer result, got: %s", result)
	}
}

func TestHierarchicalManager_NilFuncs(t *testing.T) {
	mgr := NewHierarchicalManager(ManagerConfig{})
	_, err := mgr.Run(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for nil funcs")
	}
}

func TestHierarchicalManager_DelegateError(t *testing.T) {
	cfg := ManagerConfig{
		Agents: []AgentSpec{
			{Name: "coder", Role: "developer", Description: "Writes code"},
		},
	}

	mgr := NewHierarchicalManager(cfg)
	mgr.DecomposeFunc = func(_ context.Context, _ string) (string, error) {
		return "plan", nil
	}
	mgr.DelegateFunc = func(_ context.Context, _ string, _ string) (string, error) {
		return "", context.DeadlineExceeded
	}

	result, err := mgr.Run(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	// Delegate errors are captured in results, not returned.
	if !strings.Contains(result, "Error:") {
		t.Errorf("expected error in result, got: %s", result)
	}
}

func TestHierarchicalManager_DefaultMaxDelegations(t *testing.T) {
	mgr := NewHierarchicalManager(ManagerConfig{})
	if mgr.config.MaxDelegations != 5 {
		t.Errorf("expected default max delegations 5, got %d", mgr.config.MaxDelegations)
	}
}

func TestFormatDecompositionPrompt(t *testing.T) {
	agents := []AgentSpec{
		{Name: "coder", Role: "dev", Description: "codes"},
	}
	prompt := FormatDecompositionPrompt("build it", agents)
	if !strings.Contains(prompt, "build it") {
		t.Error("expected task in prompt")
	}
	if !strings.Contains(prompt, "coder") {
		t.Error("expected agent name in prompt")
	}
}

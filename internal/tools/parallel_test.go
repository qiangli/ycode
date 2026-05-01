package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelAgents_Basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	var callCount atomic.Int32
	mockSpawner := func(_ context.Context, m *AgentManifest) (string, error) {
		callCount.Add(1)
		return fmt.Sprintf("Result from %s: %s", m.Type, m.Description), nil
	}
	RegisterParallelAgentsHandler(r, mockSpawner)

	spec, ok := r.Get("ParallelAgents")
	if !ok {
		t.Fatal("ParallelAgents not registered")
	}

	input := json.RawMessage(`{
		"agents": [
			{"description": "explore auth", "prompt": "search for auth code"},
			{"description": "explore db", "prompt": "search for database code"},
			{"description": "explore api", "prompt": "search for API endpoints"}
		]
	}`)

	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("ParallelAgents failed: %v", err)
	}

	if callCount.Load() != 3 {
		t.Errorf("expected 3 spawner calls, got %d", callCount.Load())
	}

	// All descriptions should appear in result.
	for _, desc := range []string{"explore auth", "explore db", "explore api"} {
		if !strings.Contains(result, desc) {
			t.Errorf("expected %q in result, got: %s", desc, result)
		}
	}

	// Results should be numbered.
	if !strings.Contains(result, "Agent 1:") || !strings.Contains(result, "Agent 3:") {
		t.Errorf("expected numbered agent headers, got: %s", result)
	}
}

func TestParallelAgents_Concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	// Each agent sleeps 200ms. If sequential, total would be 600ms+.
	// If parallel, total should be ~200ms.
	mockSpawner := func(_ context.Context, m *AgentManifest) (string, error) {
		time.Sleep(200 * time.Millisecond)
		return "done: " + m.Description, nil
	}
	RegisterParallelAgentsHandler(r, mockSpawner)

	spec, _ := r.Get("ParallelAgents")

	input := json.RawMessage(`{
		"agents": [
			{"description": "task-1", "prompt": "p1"},
			{"description": "task-2", "prompt": "p2"},
			{"description": "task-3", "prompt": "p3"}
		]
	}`)

	start := time.Now()
	result, err := spec.Handler(context.Background(), input)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("ParallelAgents failed: %v", err)
	}

	// Should complete in ~200-400ms, not 600ms+.
	if elapsed > 500*time.Millisecond {
		t.Errorf("agents should run concurrently; took %v (expected <500ms)", elapsed)
	}

	if !strings.Contains(result, "task-1") || !strings.Contains(result, "task-3") {
		t.Errorf("missing results, got: %s", result)
	}
}

func TestParallelAgents_ResultOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	// Agent 1 is slow, Agent 2 is fast — results should still be in input order.
	mockSpawner := func(_ context.Context, m *AgentManifest) (string, error) {
		if strings.Contains(m.Description, "slow") {
			time.Sleep(200 * time.Millisecond)
		}
		return "output:" + m.Description, nil
	}
	RegisterParallelAgentsHandler(r, mockSpawner)

	spec, _ := r.Get("ParallelAgents")

	input := json.RawMessage(`{
		"agents": [
			{"description": "slow-first", "prompt": "p1"},
			{"description": "fast-second", "prompt": "p2"}
		]
	}`)

	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("ParallelAgents failed: %v", err)
	}

	// Agent 1 (slow) should appear before Agent 2 (fast) in the output.
	idx1 := strings.Index(result, "slow-first")
	idx2 := strings.Index(result, "fast-second")
	if idx1 >= idx2 {
		t.Errorf("results should be in input order; slow-first at %d, fast-second at %d", idx1, idx2)
	}
}

func TestParallelAgents_AgentTypeDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	var receivedType AgentType
	mockSpawner := func(_ context.Context, m *AgentManifest) (string, error) {
		receivedType = m.Type
		return "ok", nil
	}
	RegisterParallelAgentsHandler(r, mockSpawner)

	spec, _ := r.Get("ParallelAgents")

	// No agent_type specified — should default to general-purpose.
	input := json.RawMessage(`{"agents":[{"description":"test","prompt":"test"}]}`)
	_, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("ParallelAgents failed: %v", err)
	}
	if receivedType != AgentGeneralPurpose {
		t.Errorf("expected default type %q, got %q", AgentGeneralPurpose, receivedType)
	}

	// Explicit agent_type.
	input = json.RawMessage(`{"agents":[{"description":"test","prompt":"test","agent_type":"Explore"}]}`)
	_, err = spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("ParallelAgents failed: %v", err)
	}
	if receivedType != AgentExplore {
		t.Errorf("expected type %q, got %q", AgentExplore, receivedType)
	}
}

func TestParallelAgents_Validation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterParallelAgentsHandler(r, func(_ context.Context, _ *AgentManifest) (string, error) {
		return "ok", nil
	})

	spec, _ := r.Get("ParallelAgents")

	// Empty agents array.
	_, err := spec.Handler(context.Background(), json.RawMessage(`{"agents":[]}`))
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("expected 'at least one' error, got: %v", err)
	}

	// Missing description.
	_, err = spec.Handler(context.Background(), json.RawMessage(`{"agents":[{"prompt":"test"}]}`))
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Errorf("expected 'description' error, got: %v", err)
	}

	// Missing prompt.
	_, err = spec.Handler(context.Background(), json.RawMessage(`{"agents":[{"description":"test"}]}`))
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Errorf("expected 'prompt' error, got: %v", err)
	}

	// Too many agents.
	agents := make([]map[string]string, 11)
	for i := range agents {
		agents[i] = map[string]string{"description": fmt.Sprintf("d%d", i), "prompt": fmt.Sprintf("p%d", i)}
	}
	data, _ := json.Marshal(map[string]any{"agents": agents})
	_, err = spec.Handler(context.Background(), data)
	if err == nil || !strings.Contains(err.Error(), "too many") {
		t.Errorf("expected 'too many' error, got: %v", err)
	}
}

func TestParallelAgents_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	mockSpawner := func(ctx context.Context, _ *AgentManifest) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Second):
			return "should not reach", nil
		}
	}
	RegisterParallelAgentsHandler(r, mockSpawner)

	spec, _ := r.Get("ParallelAgents")

	// 500ms timeout — agents should be cancelled.
	input := json.RawMessage(`{
		"agents": [{"description": "slow", "prompt": "test"}],
		"timeout": 500
	}`)

	start := time.Now()
	_, err := spec.Handler(context.Background(), input)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error due to timeout")
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout should have triggered quickly, took %v", elapsed)
	}
}

func TestParallelAgents_OneFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	mockSpawner := func(_ context.Context, m *AgentManifest) (string, error) {
		if strings.Contains(m.Description, "fail") {
			return "", fmt.Errorf("agent error: something broke")
		}
		return "success: " + m.Description, nil
	}
	RegisterParallelAgentsHandler(r, mockSpawner)

	spec, _ := r.Get("ParallelAgents")

	input := json.RawMessage(`{
		"agents": [
			{"description": "good-task", "prompt": "do something"},
			{"description": "fail-task", "prompt": "this will fail"}
		]
	}`)

	_, err := spec.Handler(context.Background(), input)
	if err == nil {
		t.Error("expected error when one agent fails")
	}
	if !strings.Contains(err.Error(), "something broke") {
		t.Errorf("expected original error message, got: %v", err)
	}
}

func TestSpecRegistration_ParallelAgents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)

	spec, ok := r.Get("ParallelAgents")
	if !ok {
		t.Fatal("ParallelAgents spec not registered")
	}
	if spec.Category != CategoryAgent {
		t.Errorf("expected CategoryAgent, got %v", spec.Category)
	}
	if !strings.Contains(spec.Description, "parallel") {
		t.Error("description should mention parallel")
	}
}

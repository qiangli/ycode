package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/agentpool"
	"github.com/qiangli/ycode/internal/runtime/task"
)

func TestAgentList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	pool := agentpool.New()
	taskReg := task.NewRegistry()
	RegisterAgentPoolHandlers(r, pool, taskReg)

	spec, ok := r.Get("AgentList")
	if !ok {
		t.Fatal("AgentList not registered")
	}

	// Empty pool.
	result, err := spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AgentList failed: %v", err)
	}
	if !strings.Contains(result, "No agents") {
		t.Errorf("expected 'No agents' for empty pool, got: %s", result)
	}

	// Add agents.
	pool.Register("agent-1234-5678", "Explore", "search codebase")
	pool.SetRunning("agent-1234-5678")
	pool.Register("agent-abcd-efgh", "Plan", "design solution")
	pool.Complete("agent-abcd-efgh", false)

	// List all.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("AgentList failed: %v", err)
	}
	if !strings.Contains(result, "agent-12") {
		t.Errorf("expected agent ID in result, got: %s", result)
	}
	if !strings.Contains(result, "Explore") {
		t.Errorf("expected agent type in result, got: %s", result)
	}

	// Active only.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{"active_only":true}`))
	if err != nil {
		t.Fatalf("AgentList active_only failed: %v", err)
	}
	if !strings.Contains(result, "Explore") {
		t.Errorf("expected active agent in result, got: %s", result)
	}
	// Completed agent should not appear.
	if strings.Contains(result, "Plan") {
		t.Errorf("completed agent should not appear in active_only, got: %s", result)
	}
}

func TestAgentWait_Completed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	pool := agentpool.New()
	taskReg := task.NewRegistry()
	RegisterAgentPoolHandlers(r, pool, taskReg)

	spec, ok := r.Get("AgentWait")
	if !ok {
		t.Fatal("AgentWait not registered")
	}

	// Create a task that completes quickly.
	tsk := taskReg.Create("test task", func(ctx context.Context) (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "task result", nil
	})

	input := json.RawMessage(`{"task_id":"` + tsk.ID + `","timeout":5000}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("AgentWait failed: %v", err)
	}
	if !strings.Contains(result, "completed") {
		t.Errorf("expected 'completed' in result, got: %s", result)
	}
}

func TestAgentWait_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	pool := agentpool.New()
	taskReg := task.NewRegistry()
	RegisterAgentPoolHandlers(r, pool, taskReg)

	spec, _ := r.Get("AgentWait")

	// Create a task that takes too long.
	tsk := taskReg.Create("slow task", func(ctx context.Context) (string, error) {
		time.Sleep(10 * time.Second)
		return "done", nil
	})

	input := json.RawMessage(`{"task_id":"` + tsk.ID + `","timeout":500}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("AgentWait failed: %v", err)
	}
	if !strings.Contains(result, "Timeout") {
		t.Errorf("expected 'Timeout' in result, got: %s", result)
	}

	// Clean up.
	_ = taskReg.Stop(tsk.ID)
}

func TestAgentClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	pool := agentpool.New()
	taskReg := task.NewRegistry()
	RegisterAgentPoolHandlers(r, pool, taskReg)

	spec, ok := r.Get("AgentClose")
	if !ok {
		t.Fatal("AgentClose not registered")
	}

	// Create a long-running task.
	tsk := taskReg.Create("long task", func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	// Close it.
	time.Sleep(50 * time.Millisecond) // Let task start.
	input := json.RawMessage(`{"task_id":"` + tsk.ID + `"}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("AgentClose failed: %v", err)
	}
	if !strings.Contains(result, "closed") {
		t.Errorf("expected 'closed' in result, got: %s", result)
	}

	// Close again — should say already stopped.
	time.Sleep(50 * time.Millisecond)
	result, err = spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("AgentClose second call failed: %v", err)
	}
	if !strings.Contains(result, "already") {
		t.Errorf("expected 'already' in result, got: %s", result)
	}
}

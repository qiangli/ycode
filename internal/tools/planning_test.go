package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/todo"
)

func TestUpdatePlan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	board := todo.NewBoard()
	oldBoard, oldPath := planBoard, planBoardPath
	SetPlanBoard(board, filepath.Join(tmpDir, "plan.json"))
	defer SetPlanBoard(oldBoard, oldPath)

	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterPlanningHandlers(r, tmpDir)

	spec, ok := r.Get("UpdatePlan")
	if !ok {
		t.Fatal("UpdatePlan not registered")
	}

	// Create steps.
	input := json.RawMessage(`{
		"steps": [
			{"title": "Research", "status": "done", "priority": 1},
			{"title": "Implement", "status": "in_progress", "priority": 2},
			{"title": "Test", "status": "pending", "priority": 3}
		]
	}`)
	result, err := spec.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdatePlan failed: %v", err)
	}
	if !strings.Contains(result, "Research") {
		t.Errorf("expected 'Research' in plan, got: %s", result)
	}
	if !strings.Contains(result, "[x]") {
		t.Errorf("expected done status icon in plan, got: %s", result)
	}
	if !strings.Contains(result, "[~]") {
		t.Errorf("expected in_progress status icon in plan, got: %s", result)
	}

	// Verify persistence.
	if _, err := os.Stat(filepath.Join(tmpDir, "plan.json")); os.IsNotExist(err) {
		t.Error("plan.json should be persisted")
	}
}

func TestListPlan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	board := todo.NewBoard()
	oldBoard, oldPath := planBoard, planBoardPath
	SetPlanBoard(board, "")
	defer SetPlanBoard(oldBoard, oldPath)

	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterPlanningHandlers(r, t.TempDir())

	spec, ok := r.Get("ListPlan")
	if !ok {
		t.Fatal("ListPlan not registered")
	}

	// Empty plan.
	result, err := spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("ListPlan failed: %v", err)
	}
	if !strings.Contains(result, "No plan") {
		t.Errorf("expected 'No plan' for empty board, got: %s", result)
	}

	// Add a step and list again.
	board.Create("Step 1", "do something", "", 1)
	result, err = spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("ListPlan failed: %v", err)
	}
	if !strings.Contains(result, "Step 1") {
		t.Errorf("expected 'Step 1' in plan, got: %s", result)
	}
}

func TestSetGoal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterPlanningHandlers(r, tmpDir)

	spec, ok := r.Get("SetGoal")
	if !ok {
		t.Fatal("SetGoal not registered")
	}

	result, err := spec.Handler(context.Background(), json.RawMessage(`{
		"objective": "Implement PDF support",
		"budget": 50000
	}`))
	if err != nil {
		t.Fatalf("SetGoal failed: %v", err)
	}
	if !strings.Contains(result, "PDF support") {
		t.Errorf("expected objective in result, got: %s", result)
	}
	if !strings.Contains(result, "50000") {
		t.Errorf("expected budget in result, got: %s", result)
	}

	// Verify file was created.
	goalPath := filepath.Join(tmpDir, ".agents", "ycode", "goal.json")
	data, err := os.ReadFile(goalPath)
	if err != nil {
		t.Fatalf("goal.json not created: %v", err)
	}
	if !strings.Contains(string(data), "PDF support") {
		t.Error("goal.json should contain the objective")
	}
}

func TestGetGoal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	tmpDir := t.TempDir()

	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterPlanningHandlers(r, tmpDir)

	// No goal set.
	spec, _ := r.Get("GetGoal")
	result, err := spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("GetGoal failed: %v", err)
	}
	if !strings.Contains(result, "No goal") {
		t.Errorf("expected 'No goal' when none set, got: %s", result)
	}

	// Set a goal then get it.
	setSpec, _ := r.Get("SetGoal")
	_, _ = setSpec.Handler(context.Background(), json.RawMessage(`{"objective":"Build agent pool"}`))

	result, err = spec.Handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("GetGoal after set failed: %v", err)
	}
	if !strings.Contains(result, "Build agent pool") {
		t.Errorf("expected objective in result, got: %s", result)
	}
}

func TestSetTaskStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	r := NewRegistry()
	RegisterBuiltins(r)
	RegisterPlanningHandlers(r, t.TempDir())

	spec, ok := r.Get("SetTaskStatus")
	if !ok {
		t.Fatal("SetTaskStatus not registered")
	}

	// Valid status.
	result, err := spec.Handler(context.Background(), json.RawMessage(`{"status":"WORKING","message":"implementing PDF reader"}`))
	if err != nil {
		t.Fatalf("SetTaskStatus failed: %v", err)
	}
	if !strings.Contains(result, "WORKING") {
		t.Errorf("expected 'WORKING' in result, got: %s", result)
	}
	if !strings.Contains(result, "PDF reader") {
		t.Errorf("expected message in result, got: %s", result)
	}

	// Invalid status.
	_, err = spec.Handler(context.Background(), json.RawMessage(`{"status":"INVALID"}`))
	if err == nil {
		t.Error("expected error for invalid status")
	}

	// Case insensitive.
	result, err = spec.Handler(context.Background(), json.RawMessage(`{"status":"done"}`))
	if err != nil {
		t.Fatalf("SetTaskStatus lowercase failed: %v", err)
	}
	if !strings.Contains(result, "DONE") {
		t.Errorf("expected 'DONE' in result, got: %s", result)
	}
}

func TestSpecRegistration_NewFeatureTools(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	r := NewRegistry()
	RegisterBuiltins(r)

	newTools := []string{
		"read_document",
		"AgentList", "AgentWait", "AgentClose",
		"UpdatePlan", "ListPlan", "SetGoal", "GetGoal", "SetTaskStatus",
	}
	for _, name := range newTools {
		if _, ok := r.Get(name); !ok {
			t.Errorf("expected spec %q to be registered", name)
		}
	}
}

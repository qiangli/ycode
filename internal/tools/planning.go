package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/todo"
)

// planBoard is the module-level todo board, set via SetPlanBoard.
var planBoard *todo.Board

// planBoardPath is the persistence path for the plan board.
var planBoardPath string

// SetPlanBoard injects the todo board for planning tools.
func SetPlanBoard(b *todo.Board, persistPath string) {
	planBoard = b
	planBoardPath = persistPath
}

func checkPlanBoard() error {
	if planBoard == nil {
		return fmt.Errorf("plan board is not initialized")
	}
	return nil
}

func savePlanBoard() {
	if planBoard != nil && planBoardPath != "" {
		_ = os.MkdirAll(filepath.Dir(planBoardPath), 0o755)
		_ = planBoard.Save(planBoardPath)
	}
}

// RegisterPlanningHandlers registers planning and workflow tools:
// UpdatePlan, SetGoal, GetGoal, SetTaskStatus.
func RegisterPlanningHandlers(r *Registry, workDir string) {
	registerUpdatePlanHandler(r)
	registerSetGoalHandler(r, workDir)
	registerGetGoalHandler(r, workDir)
	registerSetTaskStatusHandler(r)
	registerListPlanHandler(r)
}

// UpdatePlan — update a step-by-step plan with statuses
func registerUpdatePlanHandler(r *Registry) {
	spec, ok := r.Get("UpdatePlan")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		if err := checkPlanBoard(); err != nil {
			return "", err
		}

		var params struct {
			Steps []struct {
				ID          string `json:"id,omitempty"`
				Title       string `json:"title"`
				Description string `json:"description,omitempty"`
				Status      string `json:"status"` // pending, in_progress, done, blocked
				ParentID    string `json:"parent_id,omitempty"`
				Priority    int    `json:"priority,omitempty"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse UpdatePlan input: %w", err)
		}

		for _, step := range params.Steps {
			if step.ID != "" {
				// Update existing step.
				status := todo.Status(step.Status)
				if err := planBoard.Update(step.ID, status); err != nil {
					// If not found, create it.
					item := planBoard.Create(step.Title, step.Description, step.ParentID, step.Priority)
					if step.Status != "" && step.Status != "pending" {
						_ = planBoard.Update(item.ID, status)
					}
				}
			} else {
				// Create new step.
				item := planBoard.Create(step.Title, step.Description, step.ParentID, step.Priority)
				if step.Status != "" && step.Status != "pending" {
					_ = planBoard.Update(item.ID, todo.Status(step.Status))
				}
			}
		}

		savePlanBoard()
		return planBoard.RenderMarkdown(), nil
	}
}

// SetGoal — create or update a goal with optional token budget
func registerSetGoalHandler(r *Registry, workDir string) {
	spec, ok := r.Get("SetGoal")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Objective string `json:"objective"`
			Budget    int    `json:"budget,omitempty"` // optional token budget
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse SetGoal input: %w", err)
		}
		if params.Objective == "" {
			return "", fmt.Errorf("objective is required")
		}

		goal := map[string]interface{}{
			"objective": params.Objective,
			"status":    "active",
		}
		if params.Budget > 0 {
			goal["budget"] = params.Budget
		}

		goalPath := filepath.Join(workDir, ".agents", "ycode", "goal.json")
		if err := os.MkdirAll(filepath.Dir(goalPath), 0o755); err != nil {
			return "", fmt.Errorf("create goal directory: %w", err)
		}

		data, _ := json.MarshalIndent(goal, "", "  ")
		if err := os.WriteFile(goalPath, data, 0o644); err != nil {
			return "", fmt.Errorf("write goal: %w", err)
		}

		result := fmt.Sprintf("Goal set: %s", params.Objective)
		if params.Budget > 0 {
			result += fmt.Sprintf(" (budget: %d tokens)", params.Budget)
		}
		return result, nil
	}
}

// GetGoal — retrieve the current goal
func registerGetGoalHandler(r *Registry, workDir string) {
	spec, ok := r.Get("GetGoal")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, _ json.RawMessage) (string, error) {
		goalPath := filepath.Join(workDir, ".agents", "ycode", "goal.json")
		data, err := os.ReadFile(goalPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "No goal set.", nil
			}
			return "", fmt.Errorf("read goal: %w", err)
		}
		return string(data), nil
	}
}

// SetTaskStatus — set a simple status indicator
func registerSetTaskStatusHandler(r *Registry) {
	spec, ok := r.Get("SetTaskStatus")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Status  string `json:"status"` // PLANNING, WORKING, DONE, BLOCKED
			Message string `json:"message,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse SetTaskStatus input: %w", err)
		}

		valid := map[string]bool{
			"PLANNING": true, "WORKING": true, "DONE": true, "BLOCKED": true,
		}
		status := strings.ToUpper(params.Status)
		if !valid[status] {
			return "", fmt.Errorf("invalid status %q; must be PLANNING, WORKING, DONE, or BLOCKED", params.Status)
		}

		result := fmt.Sprintf("Status: %s", status)
		if params.Message != "" {
			result += fmt.Sprintf(" — %s", params.Message)
		}
		return result, nil
	}
}

// ListPlan — show the current plan board
func registerListPlanHandler(r *Registry) {
	spec, ok := r.Get("ListPlan")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, _ json.RawMessage) (string, error) {
		if err := checkPlanBoard(); err != nil {
			return "", err
		}
		md := planBoard.RenderMarkdown()
		if md == "" {
			return "No plan steps defined yet.", nil
		}
		return md, nil
	}
}

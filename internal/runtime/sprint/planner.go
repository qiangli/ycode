package sprint

import (
	"fmt"
)

// PlanResult holds the decomposition of a goal into milestones/slices/tasks.
type PlanResult struct {
	Milestone *SprintMilestone
}

// DecomposeGoal creates a simple milestone with a single slice of tasks.
// In production, this would use an LLM to decompose the goal intelligently.
// For now, it creates a minimal structure that the runner can execute.
func DecomposeGoal(goal string, taskDescriptions []string) *PlanResult {
	tasks := make([]SprintTask, len(taskDescriptions))
	for i, desc := range taskDescriptions {
		tasks[i] = SprintTask{
			ID:          fmt.Sprintf("T%02d", i+1),
			SliceID:     "S01",
			Description: desc,
			Status:      TaskPending,
		}
	}

	return &PlanResult{
		Milestone: &SprintMilestone{
			ID:   "M01",
			Goal: goal,
			Slices: []SprintSlice{
				{
					ID:          "S01",
					MilestoneID: "M01",
					Description: goal,
					Tasks:       tasks,
				},
			},
		},
	}
}

// DecomposeWithCriteria creates tasks with acceptance criteria.
func DecomposeWithCriteria(goal string, tasks []TaskDefinition) *PlanResult {
	sprintTasks := make([]SprintTask, len(tasks))
	for i, td := range tasks {
		sprintTasks[i] = SprintTask{
			ID:                 fmt.Sprintf("T%02d", i+1),
			SliceID:            "S01",
			Description:        td.Description,
			AcceptanceCriteria: td.AcceptanceCriteria,
			Status:             TaskPending,
		}
	}

	return &PlanResult{
		Milestone: &SprintMilestone{
			ID:   "M01",
			Goal: goal,
			Slices: []SprintSlice{
				{
					ID:          "S01",
					MilestoneID: "M01",
					Description: goal,
					Tasks:       sprintTasks,
				},
			},
		},
	}
}

// TaskDefinition is the input format for task decomposition.
type TaskDefinition struct {
	Description        string
	AcceptanceCriteria []string
}

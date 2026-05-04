package sprint

import (
	"context"
)

// TaskSource provides an interface for importing tasks from external systems.
// Inspired by ralph-claude-code's multi-source task import (Beads, GitHub
// Issues, PRD files) and gastown's bead-based work sourcing.
//
// Implementations can source tasks from GitHub Issues, Linear tickets,
// markdown checklists, or any other external system.
type TaskSource interface {
	// Name returns a human-readable name for this source (e.g., "github", "linear", "markdown").
	Name() string

	// FetchTasks retrieves tasks from the external system.
	// Returns normalized task descriptions ready for sprint planning.
	FetchTasks(ctx context.Context, opts TaskSourceOpts) ([]ImportedTask, error)
}

// TaskSourceOpts configures task fetching.
type TaskSourceOpts struct {
	// ProjectID or repo identifier (e.g., "owner/repo" for GitHub).
	ProjectID string

	// Labels filters tasks by labels/tags (e.g., ["bug", "enhancement"]).
	Labels []string

	// MaxTasks limits the number of tasks returned.
	MaxTasks int

	// IncludeClosed includes completed/closed tasks if true.
	IncludeClosed bool
}

// ImportedTask is a normalized task from any external source.
type ImportedTask struct {
	// ExternalID is the ID in the source system (e.g., "#123" for GitHub).
	ExternalID string `json:"external_id"`

	// Title is the task summary.
	Title string `json:"title"`

	// Description is the full task body/description.
	Description string `json:"description"`

	// Priority is the normalized priority (high, medium, low).
	Priority TaskPriority `json:"priority"`

	// Labels are tags from the source system.
	Labels []string `json:"labels,omitempty"`

	// DependsOn lists external IDs this task depends on.
	DependsOn []string `json:"depends_on,omitempty"`

	// Source identifies which TaskSource produced this task.
	Source string `json:"source"`
}

// TaskPriority represents normalized task priority.
type TaskPriority string

const (
	PriorityHigh   TaskPriority = "high"
	PriorityMedium TaskPriority = "medium"
	PriorityLow    TaskPriority = "low"
)

// ImportToSprintTasks converts imported tasks into SprintTasks for a sprint.
// Tasks are placed into a single slice within the milestone.
func ImportToSprintTasks(imported []ImportedTask, sliceID string) []SprintTask {
	tasks := make([]SprintTask, len(imported))
	for i, imp := range imported {
		tasks[i] = SprintTask{
			ID:          imp.ExternalID,
			SliceID:     sliceID,
			Description: imp.Title + "\n" + imp.Description,
			Status:      TaskPending,
		}
	}
	return tasks
}

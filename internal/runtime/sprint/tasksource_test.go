package sprint

import (
	"context"
	"testing"
)

// mockSource implements TaskSource for testing.
type mockSource struct {
	name  string
	tasks []ImportedTask
	err   error
}

func (m *mockSource) Name() string { return m.name }

func (m *mockSource) FetchTasks(_ context.Context, _ TaskSourceOpts) ([]ImportedTask, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tasks, nil
}

func TestTaskSource_FetchAndConvert(t *testing.T) {
	src := &mockSource{
		name: "test",
		tasks: []ImportedTask{
			{ExternalID: "#1", Title: "Fix login bug", Description: "Users can't login", Priority: PriorityHigh, Source: "test"},
			{ExternalID: "#2", Title: "Add docs", Description: "Missing API docs", Priority: PriorityLow, Source: "test"},
		},
	}

	tasks, err := src.FetchTasks(context.Background(), TaskSourceOpts{})
	if err != nil {
		t.Fatalf("FetchTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks count = %d, want 2", len(tasks))
	}
	if tasks[0].Priority != PriorityHigh {
		t.Errorf("first task priority = %s, want high", tasks[0].Priority)
	}
}

func TestImportToSprintTasks(t *testing.T) {
	imported := []ImportedTask{
		{ExternalID: "#1", Title: "Fix bug", Description: "Details here"},
		{ExternalID: "#2", Title: "Add feature", Description: "More details"},
	}

	sprintTasks := ImportToSprintTasks(imported, "slice-1")
	if len(sprintTasks) != 2 {
		t.Fatalf("sprint tasks count = %d, want 2", len(sprintTasks))
	}
	if sprintTasks[0].ID != "#1" {
		t.Errorf("first task ID = %q, want #1", sprintTasks[0].ID)
	}
	if sprintTasks[0].SliceID != "slice-1" {
		t.Errorf("slice ID = %q, want slice-1", sprintTasks[0].SliceID)
	}
	if sprintTasks[0].Status != TaskPending {
		t.Errorf("status = %q, want pending", sprintTasks[0].Status)
	}
}

func TestImportToSprintTasks_Empty(t *testing.T) {
	tasks := ImportToSprintTasks(nil, "s1")
	if len(tasks) != 0 {
		t.Errorf("expected empty, got %d tasks", len(tasks))
	}
}

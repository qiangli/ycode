package todo

import (
	"strings"
	"testing"
)

func TestCreateAndGet(t *testing.T) {
	b := NewBoard()
	item := b.Create("Task 1", "Description", "", 1)

	if item.ID == "" {
		t.Fatal("ID should not be empty")
	}
	if item.Title != "Task 1" {
		t.Fatalf("title = %q", item.Title)
	}
	if item.Status != StatusPending {
		t.Fatalf("status = %q, want pending", item.Status)
	}
	if item.Priority != 1 {
		t.Fatalf("priority = %d, want 1", item.Priority)
	}

	got, ok := b.Get(item.ID)
	if !ok || got.ID != item.ID {
		t.Fatal("Get failed")
	}

	_, ok = b.Get("missing")
	if ok {
		t.Fatal("Get should return false for missing ID")
	}
}

func TestUpdate(t *testing.T) {
	b := NewBoard()
	item := b.Create("Task", "", "", 0)

	err := b.Update(item.ID, StatusInProgress)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := b.Get(item.ID)
	if got.Status != StatusInProgress {
		t.Fatalf("status = %q, want in_progress", got.Status)
	}

	err = b.Update("missing", StatusDone)
	if err == nil {
		t.Fatal("expected error for missing ID")
	}
}

func TestAssign(t *testing.T) {
	b := NewBoard()
	item := b.Create("Task", "", "", 0)

	err := b.Assign(item.ID, "agent-1")
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
	got, _ := b.Get(item.ID)
	if got.AssignedTo != "agent-1" {
		t.Fatalf("assigned_to = %q", got.AssignedTo)
	}

	err = b.Assign("missing", "agent")
	if err == nil {
		t.Fatal("expected error for missing ID")
	}
}

func TestAddDependencyAndIsReady(t *testing.T) {
	b := NewBoard()
	dep := b.Create("Dependency", "", "", 0)
	item := b.Create("Task", "", "", 0)

	err := b.AddDependency(item.ID, dep.ID)
	if err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	if b.IsReady(item.ID) {
		t.Fatal("should not be ready")
	}

	b.Update(dep.ID, StatusDone)
	if !b.IsReady(item.ID) {
		t.Fatal("should be ready after dep completed")
	}
}

func TestAddDependencyErrors(t *testing.T) {
	b := NewBoard()
	item := b.Create("Task", "", "", 0)

	err := b.AddDependency("missing", item.ID)
	if err == nil {
		t.Fatal("expected error for missing item")
	}

	err = b.AddDependency(item.ID, "missing")
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
}

func TestIsReadyNoDeps(t *testing.T) {
	b := NewBoard()
	item := b.Create("Task", "", "", 0)
	if !b.IsReady(item.ID) {
		t.Fatal("task with no deps should be ready")
	}
}

func TestIsReadyMissing(t *testing.T) {
	b := NewBoard()
	if b.IsReady("missing") {
		t.Fatal("missing item should not be ready")
	}
}

func TestGetBlocked(t *testing.T) {
	b := NewBoard()
	dep := b.Create("Dep", "", "", 0)
	blocked := b.Create("Blocked", "", "", 0)
	b.AddDependency(blocked.ID, dep.ID)
	b.Create("Free", "", "", 0)

	blockedList := b.GetBlocked()
	if len(blockedList) != 1 {
		t.Fatalf("blocked = %d, want 1", len(blockedList))
	}
	if blockedList[0].ID != blocked.ID {
		t.Fatal("wrong blocked item")
	}
}

func TestGetChildren(t *testing.T) {
	b := NewBoard()
	parent := b.Create("Parent", "", "", 0)
	child1 := b.Create("Child1", "", parent.ID, 0)
	child2 := b.Create("Child2", "", parent.ID, 0)
	b.Create("Other", "", "", 0)

	children := b.GetChildren(parent.ID)
	if len(children) != 2 {
		t.Fatalf("children = %d, want 2", len(children))
	}
	ids := map[string]bool{children[0].ID: true, children[1].ID: true}
	if !ids[child1.ID] || !ids[child2.ID] {
		t.Fatal("children IDs don't match")
	}
}

func TestGetAssignedTo(t *testing.T) {
	b := NewBoard()
	t1 := b.Create("Task1", "", "", 0)
	t2 := b.Create("Task2", "", "", 0)
	b.Create("Task3", "", "", 0)

	b.Assign(t1.ID, "agent-1")
	b.Assign(t2.ID, "agent-1")

	assigned := b.GetAssignedTo("agent-1")
	if len(assigned) != 2 {
		t.Fatalf("assigned = %d, want 2", len(assigned))
	}

	none := b.GetAssignedTo("agent-2")
	if len(none) != 0 {
		t.Fatalf("assigned to agent-2 = %d, want 0", len(none))
	}
}

func TestRenderMarkdown(t *testing.T) {
	b := NewBoard()
	if b.RenderMarkdown() != "" {
		t.Fatal("empty board should return empty string")
	}

	item := b.Create("My Task", "", "", 2)
	b.Assign(item.ID, "bot")

	md := b.RenderMarkdown()
	if !strings.Contains(md, "## Task Board") {
		t.Fatal("should contain header")
	}
	if !strings.Contains(md, "My Task") {
		t.Fatal("should contain task title")
	}
	if !strings.Contains(md, "bot") {
		t.Fatal("should contain assignee")
	}
	if !strings.Contains(md, "[ ]") {
		t.Fatal("should contain pending icon")
	}
}

func TestBoardLen(t *testing.T) {
	b := NewBoard()
	if b.Len() != 0 {
		t.Fatal("empty board len should be 0")
	}
	b.Create("A", "", "", 0)
	b.Create("B", "", "", 0)
	if b.Len() != 2 {
		t.Fatalf("len = %d, want 2", b.Len())
	}
}

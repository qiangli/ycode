package task

import (
	"testing"
)

func TestCreateRoot(t *testing.T) {
	tt := NewTaskTree()
	root := tt.CreateRoot("root task")

	if root.ID == "" {
		t.Fatal("root ID should not be empty")
	}
	if root.Description != "root task" {
		t.Fatalf("got description %q, want %q", root.Description, "root task")
	}
	if root.Status != StatusPending {
		t.Fatalf("got status %q, want %q", root.Status, StatusPending)
	}
	if root.ParentID != "" {
		t.Fatalf("root should have no parent, got %q", root.ParentID)
	}
	if root.Inbox == nil {
		t.Fatal("root inbox should not be nil")
	}
	if tt.Len() != 1 {
		t.Fatalf("tree length = %d, want 1", tt.Len())
	}
}

func TestCreateChild(t *testing.T) {
	tt := NewTaskTree()
	root := tt.CreateRoot("parent")

	child, err := tt.CreateChild(root.ID, "child task")
	if err != nil {
		t.Fatalf("CreateChild: %v", err)
	}
	if child.ParentID != root.ID {
		t.Fatalf("child parent = %q, want %q", child.ParentID, root.ID)
	}
	if child.Description != "child task" {
		t.Fatalf("child description = %q, want %q", child.Description, "child task")
	}
	if tt.Len() != 2 {
		t.Fatalf("tree length = %d, want 2", tt.Len())
	}
}

func TestCreateChildInvalidParent(t *testing.T) {
	tt := NewTaskTree()
	_, err := tt.CreateChild("nonexistent", "orphan")
	if err == nil {
		t.Fatal("expected error for invalid parent ID")
	}
}

func TestGet(t *testing.T) {
	tt := NewTaskTree()
	root := tt.CreateRoot("task")

	got, ok := tt.Get(root.ID)
	if !ok || got.ID != root.ID {
		t.Fatalf("Get(%q) failed", root.ID)
	}

	_, ok = tt.Get("missing")
	if ok {
		t.Fatal("Get should return false for missing ID")
	}
}

func TestGetChildren(t *testing.T) {
	tt := NewTaskTree()
	root := tt.CreateRoot("parent")
	c1, _ := tt.CreateChild(root.ID, "child1")
	c2, _ := tt.CreateChild(root.ID, "child2")

	children := tt.GetChildren(root.ID)
	if len(children) != 2 {
		t.Fatalf("got %d children, want 2", len(children))
	}

	ids := map[string]bool{children[0].ID: true, children[1].ID: true}
	if !ids[c1.ID] || !ids[c2.ID] {
		t.Fatal("children IDs don't match")
	}

	// Non-existent parent returns nil.
	if got := tt.GetChildren("missing"); got != nil {
		t.Fatalf("expected nil for missing parent, got %v", got)
	}
}

func TestGetParent(t *testing.T) {
	tt := NewTaskTree()
	root := tt.CreateRoot("parent")
	child, _ := tt.CreateChild(root.ID, "child")

	parent, ok := tt.GetParent(child.ID)
	if !ok || parent.ID != root.ID {
		t.Fatal("GetParent should return the root")
	}

	// Root has no parent.
	_, ok = tt.GetParent(root.ID)
	if ok {
		t.Fatal("root should have no parent")
	}

	// Missing node.
	_, ok = tt.GetParent("missing")
	if ok {
		t.Fatal("missing node should return false")
	}
}

func TestSetStatus(t *testing.T) {
	tt := NewTaskTree()
	root := tt.CreateRoot("task")

	tt.SetStatus(root.ID, StatusRunning)
	got, _ := tt.Get(root.ID)
	if got.Status != StatusRunning {
		t.Fatalf("status = %q, want %q", got.Status, StatusRunning)
	}

	// SetStatus on missing ID is a no-op.
	tt.SetStatus("missing", StatusCompleted)
}

func TestRoots(t *testing.T) {
	tt := NewTaskTree()
	r1 := tt.CreateRoot("root1")
	r2 := tt.CreateRoot("root2")
	tt.CreateChild(r1.ID, "child")

	roots := tt.Roots()
	if len(roots) != 2 {
		t.Fatalf("got %d roots, want 2", len(roots))
	}
	ids := map[string]bool{roots[0].ID: true, roots[1].ID: true}
	if !ids[r1.ID] || !ids[r2.ID] {
		t.Fatal("roots IDs don't match")
	}
}

func TestLen(t *testing.T) {
	tt := NewTaskTree()
	if tt.Len() != 0 {
		t.Fatal("empty tree should have length 0")
	}
	root := tt.CreateRoot("a")
	tt.CreateChild(root.ID, "b")
	if tt.Len() != 2 {
		t.Fatalf("tree length = %d, want 2", tt.Len())
	}
}

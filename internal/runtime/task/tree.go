package task

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TaskNode extends Task with parent-child relationships and a mailbox.
type TaskNode struct {
	*Task
	ParentID string   `json:"parent_id,omitempty"`
	ChildIDs []string `json:"child_ids,omitempty"`
	Inbox    *Mailbox `json:"-"`
}

// TaskTree manages hierarchical tasks with parent-child relationships.
type TaskTree struct {
	mu    sync.RWMutex
	nodes map[string]*TaskNode
}

// NewTaskTree creates a new task tree.
func NewTaskTree() *TaskTree {
	return &TaskTree{
		nodes: make(map[string]*TaskNode),
	}
}

// CreateRoot creates a root task node (no parent).
func (tt *TaskTree) CreateRoot(description string) *TaskNode {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	node := &TaskNode{
		Task: &Task{
			ID:          uuid.New().String(),
			Description: description,
			Status:      StatusPending,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		Inbox: NewMailbox(32),
	}
	tt.nodes[node.ID] = node
	return node
}

// CreateChild creates a child task under the given parent.
func (tt *TaskTree) CreateChild(parentID, description string) (*TaskNode, error) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	parent, ok := tt.nodes[parentID]
	if !ok {
		return nil, fmt.Errorf("parent task %q not found", parentID)
	}

	node := &TaskNode{
		Task: &Task{
			ID:          uuid.New().String(),
			Description: description,
			Status:      StatusPending,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		ParentID: parentID,
		Inbox:    NewMailbox(32),
	}
	tt.nodes[node.ID] = node
	parent.ChildIDs = append(parent.ChildIDs, node.ID)
	return node, nil
}

// Get returns a task node by ID.
func (tt *TaskTree) Get(id string) (*TaskNode, bool) {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	n, ok := tt.nodes[id]
	return n, ok
}

// GetChildren returns the child nodes of a given task.
func (tt *TaskTree) GetChildren(parentID string) []*TaskNode {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	parent, ok := tt.nodes[parentID]
	if !ok {
		return nil
	}
	children := make([]*TaskNode, 0, len(parent.ChildIDs))
	for _, id := range parent.ChildIDs {
		if child, ok := tt.nodes[id]; ok {
			children = append(children, child)
		}
	}
	return children
}

// GetParent returns the parent node of a given task.
func (tt *TaskTree) GetParent(childID string) (*TaskNode, bool) {
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	child, ok := tt.nodes[childID]
	if !ok || child.ParentID == "" {
		return nil, false
	}
	parent, ok := tt.nodes[child.ParentID]
	return parent, ok
}

// SetStatus updates a node's status.
func (tt *TaskTree) SetStatus(id string, status Status) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if n, ok := tt.nodes[id]; ok {
		n.Status = status
		n.UpdatedAt = time.Now()
	}
}

// Len returns the number of nodes in the tree.
func (tt *TaskTree) Len() int {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	return len(tt.nodes)
}

// Roots returns all root nodes (no parent).
func (tt *TaskTree) Roots() []*TaskNode {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	var roots []*TaskNode
	for _, n := range tt.nodes {
		if n.ParentID == "" {
			roots = append(roots, n)
		}
	}
	return roots
}

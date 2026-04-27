// Package todo provides a hierarchical task management system with dependencies,
// assignments, and persistent state for multi-agent workflows.
package todo

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status represents a todo item's state.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusBlocked    Status = "blocked"
)

// TodoItem represents a single task in the todo system.
type TodoItem struct {
	ID           string    `json:"id"`
	ParentID     string    `json:"parent_id,omitempty"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	Status       Status    `json:"status"`
	AssignedTo   string    `json:"assigned_to,omitempty"`
	Dependencies []string  `json:"dependencies,omitempty"`
	Priority     int       `json:"priority"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Board manages hierarchical todo items.
type Board struct {
	mu    sync.RWMutex
	items map[string]*TodoItem
}

// NewBoard creates an empty todo board.
func NewBoard() *Board {
	return &Board{
		items: make(map[string]*TodoItem),
	}
}

// Create adds a new todo item.
func (b *Board) Create(title, description, parentID string, priority int) *TodoItem {
	b.mu.Lock()
	defer b.mu.Unlock()

	item := &TodoItem{
		ID:          uuid.New().String()[:8],
		ParentID:    parentID,
		Title:       title,
		Description: description,
		Status:      StatusPending,
		Priority:    priority,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	b.items[item.ID] = item
	return item
}

// Get returns a todo item by ID.
func (b *Board) Get(id string) (*TodoItem, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	item, ok := b.items[id]
	return item, ok
}

// Update modifies a todo item's status.
func (b *Board) Update(id string, status Status) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	item, ok := b.items[id]
	if !ok {
		return fmt.Errorf("todo %q not found", id)
	}
	item.Status = status
	item.UpdatedAt = time.Now()
	return nil
}

// Assign assigns a todo item to an agent.
func (b *Board) Assign(id, agentName string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	item, ok := b.items[id]
	if !ok {
		return fmt.Errorf("todo %q not found", id)
	}
	item.AssignedTo = agentName
	item.UpdatedAt = time.Now()
	return nil
}

// AddDependency adds a dependency between two items.
func (b *Board) AddDependency(id, dependsOnID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	item, ok := b.items[id]
	if !ok {
		return fmt.Errorf("todo %q not found", id)
	}
	if _, ok := b.items[dependsOnID]; !ok {
		return fmt.Errorf("dependency %q not found", dependsOnID)
	}
	item.Dependencies = append(item.Dependencies, dependsOnID)
	item.UpdatedAt = time.Now()
	return nil
}

// List returns all items.
func (b *Board) List() []*TodoItem {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]*TodoItem, 0, len(b.items))
	for _, item := range b.items {
		result = append(result, item)
	}
	return result
}

// GetChildren returns child items of a given parent.
func (b *Board) GetChildren(parentID string) []*TodoItem {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var children []*TodoItem
	for _, item := range b.items {
		if item.ParentID == parentID {
			children = append(children, item)
		}
	}
	return children
}

// GetBlocked returns items that are blocked by unfinished dependencies.
func (b *Board) GetBlocked() []*TodoItem {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var blocked []*TodoItem
	for _, item := range b.items {
		if len(item.Dependencies) > 0 && !b.allDepsComplete(item) {
			blocked = append(blocked, item)
		}
	}
	return blocked
}

// GetAssignedTo returns items assigned to a specific agent.
func (b *Board) GetAssignedTo(agentName string) []*TodoItem {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var assigned []*TodoItem
	for _, item := range b.items {
		if item.AssignedTo == agentName {
			assigned = append(assigned, item)
		}
	}
	return assigned
}

// IsReady returns true if all dependencies of an item are complete.
func (b *Board) IsReady(id string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	item, ok := b.items[id]
	if !ok {
		return false
	}
	return b.allDepsComplete(item)
}

func (b *Board) allDepsComplete(item *TodoItem) bool {
	for _, depID := range item.Dependencies {
		dep, ok := b.items[depID]
		if !ok || dep.Status != StatusDone {
			return false
		}
	}
	return true
}

// Len returns the number of items.
func (b *Board) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.items)
}

// RenderMarkdown renders the todo board as a markdown table for prompt injection.
func (b *Board) RenderMarkdown() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.items) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Task Board\n\n")
	sb.WriteString("| ID | Status | Priority | Assigned | Title |\n")
	sb.WriteString("|-----|--------|----------|----------|-------|\n")

	for _, item := range b.items {
		statusIcon := map[Status]string{
			StatusPending:    "[ ]",
			StatusInProgress: "[~]",
			StatusDone:       "[x]",
			StatusBlocked:    "[!]",
		}[item.Status]

		assigned := item.AssignedTo
		if assigned == "" {
			assigned = "-"
		}

		fmt.Fprintf(&sb, "| %s | %s | %d | %s | %s |\n",
			item.ID, statusIcon, item.Priority, assigned, item.Title)
	}

	return sb.String()
}

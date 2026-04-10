package team

import (
	"fmt"
	"sync"
)

// Team represents a group of parallel sub-agents.
type Team struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	TaskIDs []string `json:"task_ids"`
}

// Registry manages teams.
type Registry struct {
	mu    sync.RWMutex
	teams map[string]*Team
}

// NewRegistry creates a new team registry.
func NewRegistry() *Registry {
	return &Registry{
		teams: make(map[string]*Team),
	}
}

// Create creates a new team.
func (r *Registry) Create(id, name string) *Team {
	t := &Team{ID: id, Name: name}
	r.mu.Lock()
	r.teams[id] = t
	r.mu.Unlock()
	return t
}

// Get returns a team by ID.
func (r *Registry) Get(id string) (*Team, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.teams[id]
	return t, ok
}

// Delete removes a team.
func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.teams[id]; !ok {
		return fmt.Errorf("team %q not found", id)
	}
	delete(r.teams, id)
	return nil
}

// AddTask adds a task ID to a team.
func (r *Registry) AddTask(teamID, taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.teams[teamID]
	if !ok {
		return fmt.Errorf("team %q not found", teamID)
	}
	t.TaskIDs = append(t.TaskIDs, taskID)
	return nil
}

package worker

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// BootState represents the worker boot lifecycle.
type BootState int

const (
	StateSpawning BootState = iota
	StateTrustRequired
	StateReadyForPrompt
	StateRunning
	StateFinished
	StateFailed
)

func (s BootState) String() string {
	switch s {
	case StateSpawning:
		return "spawning"
	case StateTrustRequired:
		return "trust_required"
	case StateReadyForPrompt:
		return "ready_for_prompt"
	case StateRunning:
		return "running"
	case StateFinished:
		return "finished"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Worker represents a managed subprocess.
type Worker struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	State     BootState `json:"state"`
	Prompt    string    `json:"prompt,omitempty"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Registry manages worker instances.
type Registry struct {
	mu      sync.RWMutex
	workers map[string]*Worker
}

// NewRegistry creates a new worker registry.
func NewRegistry() *Registry {
	return &Registry{
		workers: make(map[string]*Worker),
	}
}

// Create creates a new worker.
func (r *Registry) Create(name string) *Worker {
	w := &Worker{
		ID:        uuid.New().String(),
		Name:      name,
		State:     StateSpawning,
		CreatedAt: time.Now(),
	}
	r.mu.Lock()
	r.workers[w.ID] = w
	r.mu.Unlock()
	return w
}

// Get returns a worker by ID.
func (r *Registry) Get(id string) (*Worker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.workers[id]
	return w, ok
}

// SetState updates the worker's boot state.
func (r *Registry) SetState(id string, state BootState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workers[id]
	if !ok {
		return fmt.Errorf("worker %q not found", id)
	}
	w.State = state
	return nil
}

// Terminate removes a worker.
func (r *Registry) Terminate(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.workers[id]; !ok {
		return fmt.Errorf("worker %q not found", id)
	}
	delete(r.workers, id)
	return nil
}

// List returns all workers.
func (r *Registry) List() []*Worker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Worker, 0, len(r.workers))
	for _, w := range r.workers {
		result = append(result, w)
	}
	return result
}

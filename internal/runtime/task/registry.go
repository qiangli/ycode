package task

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status represents a task's lifecycle state.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusStopped   Status = "stopped"
)

// Task represents a background task.
type Task struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Status      Status    `json:"status"`
	Output      string    `json:"output,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	cancel context.CancelFunc `json:"-"`
}

// Registry manages background tasks.
type Registry struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewRegistry creates a new task registry.
func NewRegistry() *Registry {
	return &Registry{
		tasks: make(map[string]*Task),
	}
}

// Create starts a new task.
func (r *Registry) Create(description string, runner func(ctx context.Context) (string, error)) *Task {
	ctx, cancel := context.WithCancel(context.Background())
	t := &Task{
		ID:          uuid.New().String(),
		Description: description,
		Status:      StatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		cancel:      cancel,
	}

	r.mu.Lock()
	r.tasks[t.ID] = t
	r.mu.Unlock()

	go func() {
		r.setStatus(t.ID, StatusRunning)
		output, err := runner(ctx)
		if err != nil {
			r.setError(t.ID, err.Error())
		} else {
			r.setOutput(t.ID, output)
		}
	}()

	return t
}

// Get returns a task by ID.
func (r *Registry) Get(id string) (*Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	return t, ok
}

// List returns all tasks.
func (r *Registry) List() []*Task {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Task, 0, len(r.tasks))
	for _, t := range r.tasks {
		result = append(result, t)
	}
	return result
}

// Stop cancels a running task.
func (r *Registry) Stop(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	if t.cancel != nil {
		t.cancel()
	}
	t.Status = StatusStopped
	t.UpdatedAt = time.Now()
	return nil
}

func (r *Registry) setStatus(id string, status Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tasks[id]; ok {
		t.Status = status
		t.UpdatedAt = time.Now()
	}
}

func (r *Registry) setOutput(id, output string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tasks[id]; ok {
		t.Output = output
		t.Status = StatusCompleted
		t.UpdatedAt = time.Now()
	}
}

func (r *Registry) setError(id, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tasks[id]; ok {
		t.Error = errMsg
		t.Status = StatusFailed
		t.UpdatedAt = time.Now()
	}
}

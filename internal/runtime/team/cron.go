package team

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CronEntry represents a scheduled recurring task.
type CronEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"` // cron expression or interval
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`

	cancel context.CancelFunc `json:"-"`
}

// CronRegistry manages scheduled tasks with real execution.
type CronRegistry struct {
	mu      sync.RWMutex
	entries map[string]*CronEntry
}

// NewCronRegistry creates a new cron registry.
func NewCronRegistry() *CronRegistry {
	return &CronRegistry{
		entries: make(map[string]*CronEntry),
	}
}

// Create adds a new cron entry.
func (cr *CronRegistry) Create(id, name, schedule, command string) (*CronEntry, error) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	if _, exists := cr.entries[id]; exists {
		return nil, fmt.Errorf("cron %q already exists", id)
	}
	entry := &CronEntry{
		ID:       id,
		Name:     name,
		Schedule: schedule,
		Command:  command,
		Enabled:  true,
	}
	cr.entries[id] = entry
	return entry, nil
}

// Delete removes a cron entry.
func (cr *CronRegistry) Delete(id string) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	entry, ok := cr.entries[id]
	if !ok {
		return fmt.Errorf("cron %q not found", id)
	}
	if entry.cancel != nil {
		entry.cancel()
	}
	delete(cr.entries, id)
	return nil
}

// List returns all cron entries.
func (cr *CronRegistry) List() []*CronEntry {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	result := make([]*CronEntry, 0, len(cr.entries))
	for _, e := range cr.entries {
		result = append(result, e)
	}
	return result
}

// Start begins executing a cron entry.
func (cr *CronRegistry) Start(id string, runner func(ctx context.Context) error) error {
	cr.mu.Lock()
	entry, ok := cr.entries[id]
	if !ok {
		cr.mu.Unlock()
		return fmt.Errorf("cron %q not found", id)
	}

	ctx, cancel := context.WithCancel(context.Background())
	entry.cancel = cancel
	cr.mu.Unlock()

	// Simple interval-based execution.
	interval := 10 * time.Minute // default
	if d, err := time.ParseDuration(entry.Schedule); err == nil {
		interval = d
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
				if entry.Enabled {
					_ = runner(ctx)
				}
			}
		}
	}()

	return nil
}

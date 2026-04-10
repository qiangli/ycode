package loop

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ScheduleEntry represents a scheduled task.
type ScheduleEntry struct {
	ID       string
	Interval time.Duration
	Command  string
	ctrl     *Controller
}

// Scheduler manages multiple scheduled loops.
type Scheduler struct {
	entries map[string]*ScheduleEntry
}

// NewScheduler creates a new scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{
		entries: make(map[string]*ScheduleEntry),
	}
}

// Add creates a new scheduled entry.
func (s *Scheduler) Add(id string, interval time.Duration, command string, runner Runner) error {
	if _, exists := s.entries[id]; exists {
		return fmt.Errorf("schedule %q already exists", id)
	}
	s.entries[id] = &ScheduleEntry{
		ID:       id,
		Interval: interval,
		Command:  command,
		ctrl:     NewController(interval, runner),
	}
	return nil
}

// Start begins a scheduled entry.
func (s *Scheduler) Start(ctx context.Context, id string) error {
	entry, ok := s.entries[id]
	if !ok {
		return fmt.Errorf("schedule %q not found", id)
	}
	go entry.ctrl.Start(ctx)
	return nil
}

// Stop halts a scheduled entry.
func (s *Scheduler) Stop(id string) error {
	entry, ok := s.entries[id]
	if !ok {
		return fmt.Errorf("schedule %q not found", id)
	}
	entry.ctrl.Stop()
	return nil
}

// Remove stops and removes a scheduled entry.
func (s *Scheduler) Remove(id string) error {
	entry, ok := s.entries[id]
	if !ok {
		return fmt.Errorf("schedule %q not found", id)
	}
	entry.ctrl.Stop()
	delete(s.entries, id)
	return nil
}

// List returns all scheduled entries.
func (s *Scheduler) List() []*ScheduleEntry {
	result := make([]*ScheduleEntry, 0, len(s.entries))
	for _, e := range s.entries {
		result = append(result, e)
	}
	return result
}

// ParseInterval parses interval strings like "5m", "1h", "30s".
func ParseInterval(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 10 * time.Minute, nil // default
	}

	// Try standard duration parsing.
	d, err := time.ParseDuration(s)
	if err == nil {
		return d, nil
	}

	// Try simple number (assumed minutes).
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Minute, nil
	}

	return 0, fmt.Errorf("invalid interval: %s", s)
}

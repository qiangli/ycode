package lanes

import (
	"context"
	"fmt"
	"sync"
)

// Lane identifies an execution lane.
type Lane string

const (
	LaneMain     Lane = "main"     // primary interactive conversation
	LaneCron     Lane = "cron"     // scheduled/background tasks
	LaneSubagent Lane = "subagent" // subagent delegation work
)

// String returns the lane name.
func (l Lane) String() string {
	return string(l)
}

// Scheduler manages lane-based execution to prevent concurrency conflicts.
// Each lane serializes work items — only one item per lane runs at a time.
type Scheduler struct {
	mu    sync.Mutex
	lanes map[Lane]*laneState
}

type laneState struct {
	mu      sync.Mutex
	active  bool
	current string // description of current work item
}

// NewScheduler creates a lane scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{
		lanes: map[Lane]*laneState{
			LaneMain:     {},
			LaneCron:     {},
			LaneSubagent: {},
		},
	}
}

// Acquire claims a lane for exclusive execution.
// It blocks until the lane is available or the context is cancelled.
// Returns a release function that must be called when done.
func (s *Scheduler) Acquire(ctx context.Context, lane Lane, description string) (release func(), err error) {
	ls := s.getLane(lane)

	// Use a channel to wait for the lane to become available.
	acquired := make(chan struct{})
	go func() {
		ls.mu.Lock()
		close(acquired)
	}()

	select {
	case <-acquired:
		ls.active = true
		ls.current = description
		return func() {
			ls.current = ""
			ls.active = false
			ls.mu.Unlock()
		}, nil
	case <-ctx.Done():
		// If we acquired the lock in the goroutine but context was cancelled,
		// we need to handle both cases. Use a select to check.
		select {
		case <-acquired:
			// We got the lock but context was cancelled — release it.
			ls.mu.Unlock()
		default:
			// Lock not yet acquired — the goroutine will eventually get it.
			// Wait for it and release immediately.
			go func() {
				<-acquired
				ls.mu.Unlock()
			}()
		}
		return nil, ctx.Err()
	}
}

// TryAcquire attempts to claim a lane without blocking.
// Returns (release, true) if acquired, (nil, false) if the lane is busy.
func (s *Scheduler) TryAcquire(lane Lane, description string) (release func(), ok bool) {
	ls := s.getLane(lane)

	if !ls.mu.TryLock() {
		return nil, false
	}

	ls.active = true
	ls.current = description
	return func() {
		ls.current = ""
		ls.active = false
		ls.mu.Unlock()
	}, true
}

// IsActive returns whether a lane currently has work running.
func (s *Scheduler) IsActive(lane Lane) bool {
	ls := s.getLane(lane)
	// We can't check the mutex state directly, so use the active flag.
	// This is best-effort — the active flag is set/cleared under the lock.
	return ls.active
}

// ActiveWork returns a description of what each active lane is doing.
func (s *Scheduler) ActiveWork() map[Lane]string {
	result := make(map[Lane]string)
	s.mu.Lock()
	for lane, ls := range s.lanes {
		if ls.active {
			result[lane] = ls.current
		}
	}
	s.mu.Unlock()
	return result
}

func (s *Scheduler) getLane(lane Lane) *laneState {
	s.mu.Lock()
	defer s.mu.Unlock()
	ls, ok := s.lanes[lane]
	if !ok {
		ls = &laneState{}
		s.lanes[lane] = ls
	}
	return ls
}

// Route determines which lane a work item belongs to based on its source.
func Route(source string) Lane {
	switch source {
	case "cron", "schedule", "timer":
		return LaneCron
	case "subagent", "agent", "delegation":
		return LaneSubagent
	default:
		return LaneMain
	}
}

// LaneError is returned when a lane operation fails.
type LaneError struct {
	Lane    Lane
	Message string
}

func (e *LaneError) Error() string {
	return fmt.Sprintf("lane %s: %s", e.Lane, e.Message)
}

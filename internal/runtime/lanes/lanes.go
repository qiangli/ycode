package lanes

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

// DefaultSubagentConcurrency is the default number of concurrent subagents.
const DefaultSubagentConcurrency = 4

// Scheduler manages lane-based execution to prevent concurrency conflicts.
// Most lanes serialize work items (only one at a time), but the subagent lane
// uses bounded concurrency (multiple concurrent items up to a limit).
type Scheduler struct {
	mu    sync.Mutex
	lanes map[Lane]*laneState
}

type laneState struct {
	mu      sync.Mutex    // used for serialized lanes
	sem     chan struct{}  // used for pooled lanes (nil = use mutex)
	active  int32         // number of active work items
	current string        // description of current work item (last acquired)
}

// NewScheduler creates a lane scheduler with default concurrency limits.
func NewScheduler() *Scheduler {
	return NewSchedulerWithLimits(DefaultSubagentConcurrency)
}

// NewSchedulerWithLimits creates a lane scheduler with a configurable
// subagent concurrency limit. Main and Cron lanes remain serialized.
func NewSchedulerWithLimits(maxSubagents int) *Scheduler {
	if maxSubagents <= 0 {
		maxSubagents = DefaultSubagentConcurrency
	}
	return &Scheduler{
		lanes: map[Lane]*laneState{
			LaneMain:     {},
			LaneCron:     {},
			LaneSubagent: {sem: make(chan struct{}, maxSubagents)},
		},
	}
}

// Acquire claims a lane for execution.
// For serialized lanes (Main, Cron), it blocks until the lane is free.
// For pooled lanes (Subagent), it blocks until a slot is available.
// Returns a release function that must be called when done.
func (s *Scheduler) Acquire(ctx context.Context, lane Lane, description string) (release func(), err error) {
	ls := s.getLane(lane)

	// Pooled lane — use semaphore for bounded concurrency.
	if ls.sem != nil {
		select {
		case ls.sem <- struct{}{}:
			atomic.AddInt32(&ls.active, 1)
			s.mu.Lock()
			ls.current = description
			s.mu.Unlock()
			return func() {
				atomic.AddInt32(&ls.active, -1)
				<-ls.sem
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Serialized lane — use mutex.
	acquired := make(chan struct{})
	go func() {
		ls.mu.Lock()
		close(acquired)
	}()

	select {
	case <-acquired:
		atomic.StoreInt32(&ls.active, 1)
		ls.current = description
		return func() {
			ls.current = ""
			atomic.StoreInt32(&ls.active, 0)
			ls.mu.Unlock()
		}, nil
	case <-ctx.Done():
		select {
		case <-acquired:
			ls.mu.Unlock()
		default:
			go func() {
				<-acquired
				ls.mu.Unlock()
			}()
		}
		return nil, ctx.Err()
	}
}

// TryAcquire attempts to claim a lane without blocking.
// Returns (release, true) if acquired, (nil, false) if the lane is busy/full.
func (s *Scheduler) TryAcquire(lane Lane, description string) (release func(), ok bool) {
	ls := s.getLane(lane)

	// Pooled lane.
	if ls.sem != nil {
		select {
		case ls.sem <- struct{}{}:
			atomic.AddInt32(&ls.active, 1)
			s.mu.Lock()
			ls.current = description
			s.mu.Unlock()
			return func() {
				atomic.AddInt32(&ls.active, -1)
				<-ls.sem
			}, true
		default:
			return nil, false
		}
	}

	// Serialized lane.
	if !ls.mu.TryLock() {
		return nil, false
	}

	atomic.StoreInt32(&ls.active, 1)
	ls.current = description
	return func() {
		ls.current = ""
		atomic.StoreInt32(&ls.active, 0)
		ls.mu.Unlock()
	}, true
}

// IsActive returns whether a lane currently has work running.
func (s *Scheduler) IsActive(lane Lane) bool {
	ls := s.getLane(lane)
	return atomic.LoadInt32(&ls.active) > 0
}

// ActiveCount returns how many work items are running in a lane.
func (s *Scheduler) ActiveCount(lane Lane) int {
	ls := s.getLane(lane)
	return int(atomic.LoadInt32(&ls.active))
}

// ActiveWork returns a description of what each active lane is doing.
func (s *Scheduler) ActiveWork() map[Lane]string {
	result := make(map[Lane]string)
	s.mu.Lock()
	for lane, ls := range s.lanes {
		if atomic.LoadInt32(&ls.active) > 0 {
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

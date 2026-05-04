package autoloop

import (
	"log/slog"
	"sync"
	"time"
)

// WakeupSource classifies why a wakeup was requested.
type WakeupSource int

const (
	// WakeupScheduled is a timer-based periodic wakeup.
	WakeupScheduled WakeupSource = iota
	// WakeupTrigger is an event-driven trigger (e.g., file change, CI completion).
	WakeupTrigger
	// WakeupOnDemand is a user/API-initiated manual wakeup.
	WakeupOnDemand
)

// String returns a human-readable source label.
func (s WakeupSource) String() string {
	switch s {
	case WakeupScheduled:
		return "scheduled"
	case WakeupTrigger:
		return "trigger"
	case WakeupOnDemand:
		return "on_demand"
	default:
		return "unknown"
	}
}

// priority returns the ordering priority (higher = more urgent).
func (s WakeupSource) priority() int {
	switch s {
	case WakeupOnDemand:
		return 3
	case WakeupTrigger:
		return 2
	case WakeupScheduled:
		return 1
	default:
		return 0
	}
}

// WakeupRequest represents a pending wakeup for an agent.
type WakeupRequest struct {
	AgentID     string
	Source      WakeupSource
	RequestedAt time.Time
	Metadata    map[string]string // optional context (e.g., trigger event)
}

// WakeupStatus tracks the state of a wakeup request.
type WakeupStatus int

const (
	WakeupQueued WakeupStatus = iota
	WakeupClaimed
	WakeupCoalesced // merged into another pending request
	WakeupCompleted
	WakeupFailed
)

// String returns a human-readable status label.
func (s WakeupStatus) String() string {
	switch s {
	case WakeupQueued:
		return "queued"
	case WakeupClaimed:
		return "claimed"
	case WakeupCoalesced:
		return "coalesced"
	case WakeupCompleted:
		return "completed"
	case WakeupFailed:
		return "failed"
	default:
		return "unknown"
	}
}

type queueEntry struct {
	Request WakeupRequest
	Status  WakeupStatus
}

// WakeupQueue manages durable wakeup scheduling with coalescing.
// Ensures max-1-concurrent-run per agent and deduplicates identical
// pending requests.
//
// Inspired by paperclip's DB-backed agent_wakeup_requests table with
// coalesce semantics and priority ordering.
type WakeupQueue struct {
	mu      sync.Mutex
	entries []queueEntry
	running map[string]bool // agentID → true if currently running
	logger  *slog.Logger

	// CoalesceWindow is the time window within which duplicate requests
	// for the same (agentID, source) are coalesced.
	CoalesceWindow time.Duration
}

// NewWakeupQueue creates a wakeup queue.
func NewWakeupQueue(coalesceWindow time.Duration) *WakeupQueue {
	if coalesceWindow <= 0 {
		coalesceWindow = 30 * time.Second
	}
	return &WakeupQueue{
		running:        make(map[string]bool),
		logger:         slog.Default(),
		CoalesceWindow: coalesceWindow,
	}
}

// Enqueue adds a wakeup request. If an identical pending request exists
// within the coalesce window, the new request is coalesced (deduplicated).
// Returns true if the request was actually enqueued (not coalesced).
func (q *WakeupQueue) Enqueue(req WakeupRequest) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now()
	}

	// Check for coalescing: same agentID and source within the window.
	for i := range q.entries {
		e := &q.entries[i]
		if e.Status != WakeupQueued {
			continue
		}
		if e.Request.AgentID == req.AgentID && e.Request.Source == req.Source {
			if req.RequestedAt.Sub(e.Request.RequestedAt) < q.CoalesceWindow {
				q.logger.Info("wakeup coalesced",
					"agent_id", req.AgentID,
					"source", req.Source.String(),
				)
				return false
			}
		}
	}

	q.entries = append(q.entries, queueEntry{
		Request: req,
		Status:  WakeupQueued,
	})

	q.logger.Info("wakeup enqueued",
		"agent_id", req.AgentID,
		"source", req.Source.String(),
		"queue_size", q.queuedCount(),
	)
	return true
}

// Claim returns the next highest-priority pending wakeup, if any, and marks
// it as claimed. Respects max-1-concurrent per agent: skips agents that
// already have a running wakeup.
func (q *WakeupQueue) Claim() (WakeupRequest, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	bestIdx := -1
	bestPriority := -1

	for i := range q.entries {
		e := &q.entries[i]
		if e.Status != WakeupQueued {
			continue
		}
		if q.running[e.Request.AgentID] {
			continue // already running
		}
		p := e.Request.Source.priority()
		if p > bestPriority || (p == bestPriority && (bestIdx == -1 || e.Request.RequestedAt.Before(q.entries[bestIdx].Request.RequestedAt))) {
			bestIdx = i
			bestPriority = p
		}
	}

	if bestIdx < 0 {
		return WakeupRequest{}, false
	}

	q.entries[bestIdx].Status = WakeupClaimed
	q.running[q.entries[bestIdx].Request.AgentID] = true
	return q.entries[bestIdx].Request, true
}

// Complete marks a running wakeup as completed or failed.
func (q *WakeupQueue) Complete(agentID string, failed bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.running, agentID)

	// Update the most recent claimed entry for this agent.
	for i := len(q.entries) - 1; i >= 0; i-- {
		e := &q.entries[i]
		if e.Request.AgentID == agentID && e.Status == WakeupClaimed {
			if failed {
				e.Status = WakeupFailed
			} else {
				e.Status = WakeupCompleted
			}
			break
		}
	}
}

// QueuedCount returns the number of pending requests.
func (q *WakeupQueue) QueuedCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.queuedCount()
}

func (q *WakeupQueue) queuedCount() int {
	count := 0
	for i := range q.entries {
		if q.entries[i].Status == WakeupQueued {
			count++
		}
	}
	return count
}

// RunningCount returns the number of currently running wakeups.
func (q *WakeupQueue) RunningCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.running)
}

// Drain removes all completed/failed/coalesced entries to prevent unbounded growth.
func (q *WakeupQueue) Drain() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	kept := q.entries[:0]
	removed := 0
	for _, e := range q.entries {
		if e.Status == WakeupQueued || e.Status == WakeupClaimed {
			kept = append(kept, e)
		} else {
			removed++
		}
	}
	q.entries = kept
	return removed
}

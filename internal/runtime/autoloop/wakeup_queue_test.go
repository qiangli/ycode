package autoloop

import (
	"testing"
	"time"
)

func TestWakeupQueue_EnqueueAndClaim(t *testing.T) {
	q := NewWakeupQueue(30 * time.Second)

	req := WakeupRequest{AgentID: "agent-1", Source: WakeupScheduled}
	if !q.Enqueue(req) {
		t.Error("expected first enqueue to succeed")
	}
	if q.QueuedCount() != 1 {
		t.Errorf("queued = %d, want 1", q.QueuedCount())
	}

	claimed, ok := q.Claim()
	if !ok {
		t.Fatal("expected claim to succeed")
	}
	if claimed.AgentID != "agent-1" {
		t.Errorf("claimed agent = %q, want agent-1", claimed.AgentID)
	}
	if q.RunningCount() != 1 {
		t.Errorf("running = %d, want 1", q.RunningCount())
	}
}

func TestWakeupQueue_Coalescing(t *testing.T) {
	q := NewWakeupQueue(30 * time.Second)
	now := time.Now()

	req1 := WakeupRequest{AgentID: "a1", Source: WakeupScheduled, RequestedAt: now}
	req2 := WakeupRequest{AgentID: "a1", Source: WakeupScheduled, RequestedAt: now.Add(5 * time.Second)}

	if !q.Enqueue(req1) {
		t.Error("expected first enqueue to succeed")
	}
	if q.Enqueue(req2) {
		t.Error("expected second enqueue to be coalesced")
	}
	if q.QueuedCount() != 1 {
		t.Errorf("queued = %d, want 1 (coalesced)", q.QueuedCount())
	}
}

func TestWakeupQueue_CoalesceWindowExpired(t *testing.T) {
	q := NewWakeupQueue(10 * time.Second)
	now := time.Now()

	req1 := WakeupRequest{AgentID: "a1", Source: WakeupScheduled, RequestedAt: now}
	req2 := WakeupRequest{AgentID: "a1", Source: WakeupScheduled, RequestedAt: now.Add(15 * time.Second)}

	q.Enqueue(req1)
	if !q.Enqueue(req2) {
		t.Error("expected enqueue after coalesce window to succeed")
	}
	if q.QueuedCount() != 2 {
		t.Errorf("queued = %d, want 2", q.QueuedCount())
	}
}

func TestWakeupQueue_DifferentSourceNotCoalesced(t *testing.T) {
	q := NewWakeupQueue(30 * time.Second)

	req1 := WakeupRequest{AgentID: "a1", Source: WakeupScheduled}
	req2 := WakeupRequest{AgentID: "a1", Source: WakeupOnDemand}

	q.Enqueue(req1)
	if !q.Enqueue(req2) {
		t.Error("different sources should not coalesce")
	}
}

func TestWakeupQueue_MaxOneConcurrent(t *testing.T) {
	q := NewWakeupQueue(30 * time.Second)

	q.Enqueue(WakeupRequest{AgentID: "a1", Source: WakeupScheduled})
	q.Enqueue(WakeupRequest{AgentID: "a1", Source: WakeupOnDemand, RequestedAt: time.Now().Add(time.Minute)})

	// Claim first.
	_, ok := q.Claim()
	if !ok {
		t.Fatal("expected first claim to succeed")
	}

	// Second claim for same agent should fail (already running).
	_, ok = q.Claim()
	if ok {
		t.Error("expected second claim for same agent to fail while first is running")
	}

	// Complete first; second should now be claimable.
	q.Complete("a1", false)
	_, ok = q.Claim()
	if !ok {
		t.Error("expected claim after completion to succeed")
	}
}

func TestWakeupQueue_PriorityOrdering(t *testing.T) {
	q := NewWakeupQueue(30 * time.Second)

	// Enqueue lower priority first.
	q.Enqueue(WakeupRequest{AgentID: "a1", Source: WakeupScheduled})
	q.Enqueue(WakeupRequest{AgentID: "a2", Source: WakeupOnDemand})

	// OnDemand should be claimed first (higher priority).
	claimed, ok := q.Claim()
	if !ok {
		t.Fatal("expected claim")
	}
	if claimed.AgentID != "a2" {
		t.Errorf("expected on_demand agent a2 first, got %q", claimed.AgentID)
	}
}

func TestWakeupQueue_CompleteFailed(t *testing.T) {
	q := NewWakeupQueue(30 * time.Second)
	q.Enqueue(WakeupRequest{AgentID: "a1", Source: WakeupScheduled})
	q.Claim()
	q.Complete("a1", true)

	if q.RunningCount() != 0 {
		t.Errorf("running = %d, want 0 after failed complete", q.RunningCount())
	}
}

func TestWakeupQueue_Drain(t *testing.T) {
	q := NewWakeupQueue(30 * time.Second)

	q.Enqueue(WakeupRequest{AgentID: "a1", Source: WakeupScheduled})
	q.Enqueue(WakeupRequest{AgentID: "a2", Source: WakeupScheduled})
	q.Claim()
	q.Complete("a1", false)

	removed := q.Drain()
	if removed != 1 {
		t.Errorf("drained = %d, want 1", removed)
	}
	// a2 should still be queued.
	if q.QueuedCount() != 1 {
		t.Errorf("queued after drain = %d, want 1", q.QueuedCount())
	}
}

func TestWakeupSource_String(t *testing.T) {
	tests := []struct {
		s    WakeupSource
		want string
	}{
		{WakeupScheduled, "scheduled"},
		{WakeupTrigger, "trigger"},
		{WakeupOnDemand, "on_demand"},
		{WakeupSource(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("WakeupSource(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestWakeupStatus_String(t *testing.T) {
	tests := []struct {
		s    WakeupStatus
		want string
	}{
		{WakeupQueued, "queued"},
		{WakeupClaimed, "claimed"},
		{WakeupCoalesced, "coalesced"},
		{WakeupCompleted, "completed"},
		{WakeupFailed, "failed"},
		{WakeupStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("WakeupStatus(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

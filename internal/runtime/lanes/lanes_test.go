package lanes

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestScheduler_AcquireAndRelease(t *testing.T) {
	s := NewScheduler()

	release, err := s.Acquire(context.Background(), LaneMain, "test work")
	if err != nil {
		t.Fatal(err)
	}

	if !s.IsActive(LaneMain) {
		t.Error("expected main lane to be active")
	}

	release()

	if s.IsActive(LaneMain) {
		t.Error("expected main lane to be inactive after release")
	}
}

func TestScheduler_SerializesWork(t *testing.T) {
	s := NewScheduler()

	var order []int
	var mu sync.Mutex

	// Acquire the lane first.
	release1, _ := s.Acquire(context.Background(), LaneMain, "first")

	done := make(chan struct{})
	go func() {
		release2, _ := s.Acquire(context.Background(), LaneMain, "second")
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
		release2()
		close(done)
	}()

	// Give goroutine time to block on acquire.
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	order = append(order, 1)
	mu.Unlock()
	release1()

	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("expected [1, 2], got %v", order)
	}
}

func TestScheduler_TryAcquire(t *testing.T) {
	s := NewScheduler()

	release, ok := s.TryAcquire(LaneCron, "cron job")
	if !ok {
		t.Fatal("expected successful TryAcquire")
	}

	// Second try should fail.
	_, ok = s.TryAcquire(LaneCron, "another job")
	if ok {
		t.Error("expected TryAcquire to fail when lane is busy")
	}

	release()

	// Now it should succeed again.
	release2, ok := s.TryAcquire(LaneCron, "retry job")
	if !ok {
		t.Error("expected TryAcquire to succeed after release")
	}
	release2()
}

func TestScheduler_ContextCancellation(t *testing.T) {
	s := NewScheduler()

	// Hold the lane.
	release, _ := s.Acquire(context.Background(), LaneMain, "blocking")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := s.Acquire(ctx, LaneMain, "waiting")
	if err == nil {
		t.Error("expected error from cancelled context")
	}

	release()
}

func TestScheduler_IndependentLanes(t *testing.T) {
	s := NewScheduler()

	release1, _ := s.Acquire(context.Background(), LaneMain, "main work")
	release2, err := s.Acquire(context.Background(), LaneCron, "cron work")
	if err != nil {
		t.Fatal("cron lane should be independent from main")
	}

	if !s.IsActive(LaneMain) || !s.IsActive(LaneCron) {
		t.Error("both lanes should be active")
	}

	release1()
	release2()
}

func TestRoute(t *testing.T) {
	tests := []struct {
		source string
		want   Lane
	}{
		{"cron", LaneCron},
		{"schedule", LaneCron},
		{"subagent", LaneSubagent},
		{"agent", LaneSubagent},
		{"delegation", LaneSubagent},
		{"user", LaneMain},
		{"", LaneMain},
	}
	for _, tt := range tests {
		if got := Route(tt.source); got != tt.want {
			t.Errorf("Route(%q) = %s, want %s", tt.source, got, tt.want)
		}
	}
}

func TestScheduler_ActiveWork(t *testing.T) {
	s := NewScheduler()

	release, _ := s.Acquire(context.Background(), LaneMain, "doing stuff")
	defer release()

	work := s.ActiveWork()
	if work[LaneMain] != "doing stuff" {
		t.Errorf("expected 'doing stuff', got %q", work[LaneMain])
	}
	if _, ok := work[LaneCron]; ok {
		t.Error("cron lane should not appear in active work")
	}
}

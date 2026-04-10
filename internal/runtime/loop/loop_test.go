package loop

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestController_StartStop(t *testing.T) {
	var iterations atomic.Int32
	ctrl := NewController(10*time.Millisecond, func(ctx context.Context, iteration int) error {
		iterations.Add(1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go ctrl.Start(ctx)

	// Wait for a few iterations.
	time.Sleep(60 * time.Millisecond)
	ctrl.Stop()

	if iterations.Load() == 0 {
		t.Error("expected at least one iteration")
	}
}

func TestController_PauseResume(t *testing.T) {
	var iterations atomic.Int32
	ctrl := NewController(10*time.Millisecond, func(ctx context.Context, iteration int) error {
		iterations.Add(1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go ctrl.Start(ctx)

	time.Sleep(30 * time.Millisecond)
	ctrl.Pause()
	if ctrl.GetState() != StatePaused {
		t.Error("expected paused state")
	}

	pausedCount := iterations.Load()
	time.Sleep(30 * time.Millisecond)
	if iterations.Load() != pausedCount {
		t.Error("iterations should not increase while paused")
	}

	ctrl.Resume()
	time.Sleep(30 * time.Millisecond)
	ctrl.Stop()

	if iterations.Load() <= pausedCount {
		t.Error("iterations should increase after resume")
	}
}

func TestParseInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"5m", 5 * time.Minute, false},
		{"1h", time.Hour, false},
		{"30s", 30 * time.Second, false},
		{"", 10 * time.Minute, false}, // default
		{"5", 5 * time.Minute, false}, // plain number = minutes
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		d, err := ParseInterval(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseInterval(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseInterval(%q): %v", tt.input, err)
			continue
		}
		if d != tt.expected {
			t.Errorf("ParseInterval(%q) = %v, want %v", tt.input, d, tt.expected)
		}
	}
}

func TestContextCarryover(t *testing.T) {
	dir := t.TempDir()
	cc, err := NewContextCarryover(dir)
	if err != nil {
		t.Fatalf("new context carryover: %v", err)
	}

	cc.BeforeRun(1)
	cc.AfterRun(500*time.Millisecond, nil)

	ctx := cc.Context()
	if ctx.TotalRuns != 1 {
		t.Errorf("expected 1 total run, got %d", ctx.TotalRuns)
	}
	if ctx.SuccessCount != 1 {
		t.Errorf("expected 1 success, got %d", ctx.SuccessCount)
	}

	summary := cc.Summary()
	if summary == "" {
		t.Error("summary should not be empty")
	}
}

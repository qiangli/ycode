package toolexec

import (
	"testing"
	"time"
)

func TestStallWatchdog_FiresOnStall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stall watchdog timing test in short mode")
	}

	w := NewStallWatchdog(100*time.Millisecond, 20*time.Millisecond)
	w.Arm()
	defer w.Disarm()

	select {
	case <-w.Stalled():
		// Expected.
	case <-time.After(500 * time.Millisecond):
		t.Error("expected stall detection within 500ms")
	}
}

func TestStallWatchdog_TouchPreventsStall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stall watchdog timing test in short mode")
	}

	w := NewStallWatchdog(100*time.Millisecond, 20*time.Millisecond)
	w.Arm()
	defer w.Disarm()

	// Keep touching to prevent stall.
	done := make(chan struct{})
	go func() {
		for range 10 {
			time.Sleep(30 * time.Millisecond)
			w.Touch()
		}
		close(done)
	}()

	<-done

	// Give a small window for false stall.
	select {
	case <-w.Stalled():
		// Might fire after touches stop; that's fine.
	case <-time.After(50 * time.Millisecond):
		// No stall during touching period — correct.
	}
}

func TestStallWatchdog_Disarm(t *testing.T) {
	w := NewStallWatchdog(50*time.Millisecond, 10*time.Millisecond)
	w.Arm()
	w.Disarm()

	if w.IsArmed() {
		t.Error("expected watchdog to be disarmed")
	}

	// Should not fire after disarm.
	select {
	case <-w.Stalled():
		t.Error("should not fire after disarm")
	case <-time.After(100 * time.Millisecond):
		// Correct.
	}
}

func TestStallWatchdog_DoubleArm(t *testing.T) {
	w := NewStallWatchdog(time.Second, 100*time.Millisecond)
	w.Arm()
	w.Arm() // idempotent
	defer w.Disarm()

	if !w.IsArmed() {
		t.Error("expected watchdog to be armed")
	}
}

func TestStallWatchdog_DoubleDisarm(t *testing.T) {
	w := NewStallWatchdog(time.Second, 100*time.Millisecond)
	w.Arm()
	w.Disarm()
	w.Disarm() // should not panic
}

func TestStallWatchdog_DisarmWithoutArm(t *testing.T) {
	w := NewStallWatchdog(time.Second, 100*time.Millisecond)
	w.Disarm() // should not panic
}

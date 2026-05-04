package toolexec

import (
	"sync"
	"time"
)

// StallWatchdog detects when a tool execution stalls (no activity for a
// configurable duration). It provides an arm/touch/disarm API that can be
// used across bash, container, and browser tool executions.
//
// Inspired by openclaw's armable stall watchdog with configurable timeout
// and check interval.
type StallWatchdog struct {
	mu            sync.Mutex
	timeout       time.Duration
	checkInterval time.Duration
	lastTouch     time.Time
	armed         bool
	stopCh        chan struct{}
	stallCh       chan struct{}
}

// NewStallWatchdog creates a watchdog with the given stall timeout and check interval.
func NewStallWatchdog(timeout, checkInterval time.Duration) *StallWatchdog {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if checkInterval <= 0 {
		checkInterval = time.Second
	}
	return &StallWatchdog{
		timeout:       timeout,
		checkInterval: checkInterval,
		stallCh:       make(chan struct{}, 1),
	}
}

// Arm starts the watchdog. The stall channel will receive a signal if no
// Touch() is called within the timeout. Arm is idempotent; calling it
// while already armed is a no-op.
func (w *StallWatchdog) Arm() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.armed {
		return
	}

	w.armed = true
	w.lastTouch = time.Now()
	w.stopCh = make(chan struct{})

	go w.monitor()
}

// Touch signals that activity occurred, resetting the stall timer.
func (w *StallWatchdog) Touch() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastTouch = time.Now()
}

// Disarm stops the watchdog. Safe to call multiple times or when not armed.
func (w *StallWatchdog) Disarm() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.armed {
		return
	}
	w.armed = false
	close(w.stopCh)
}

// Stalled returns a channel that receives when a stall is detected.
// The channel is buffered (size 1) and will receive at most one signal.
func (w *StallWatchdog) Stalled() <-chan struct{} {
	return w.stallCh
}

// IsArmed returns whether the watchdog is currently active.
func (w *StallWatchdog) IsArmed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.armed
}

func (w *StallWatchdog) monitor() {
	ticker := time.NewTicker(w.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			elapsed := time.Since(w.lastTouch)
			armed := w.armed
			w.mu.Unlock()

			if !armed {
				return
			}

			if elapsed >= w.timeout {
				select {
				case w.stallCh <- struct{}{}:
				default:
				}
				// Disarm after firing.
				w.Disarm()
				return
			}
		}
	}
}

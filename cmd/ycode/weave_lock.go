//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// withWeaveQueueLock takes an exclusive flock on <dir>/queue.lock,
// loads the queue, hands it to fn for mutation, saves it back, then
// releases the lock. Used for read-modify-write cycles in
// runWeaveStart (state transitions on tool exit) and concurrent
// `weave start` invocations — without this, the orchestrator's
// "background N starts in parallel" pattern produces last-write-
// wins races on queue.json that strand subagent state transitions.
//
// Blocks (LOCK_EX without LOCK_NB) until the lock is acquired so
// concurrent callers serialize cleanly. The lock file persists on
// disk; opening + flock-ing it on every call is the cheap-and-
// correct pattern (mtime / fd tracking would add complexity for
// no win).
func withWeaveQueueLock(dir string, fn func(*weaveQueue) error) error {
	lockPath := filepath.Join(dir, "queue.lock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("queue lock: ensure dir: %w", err)
	}
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("queue lock: open: %w", err)
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("queue lock: flock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) }()

	q, err := loadWeaveQueue(dir)
	if err != nil {
		return fmt.Errorf("queue lock: load: %w", err)
	}
	if err := fn(q); err != nil {
		return err
	}
	if err := saveWeaveQueue(dir, q); err != nil {
		return fmt.Errorf("queue lock: save: %w", err)
	}
	return nil
}

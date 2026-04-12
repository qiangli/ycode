//go:build unix

package cluster

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	defaultRetryInterval  = 3 * time.Second
	maxPromotionRetries   = 3
	promotionRetryBackoff = 5 * time.Second
)

// election manages the flock-based leader election.
type election struct {
	lockPath string
	lockFile *os.File
	isLeader bool
}

func newElection(baseDir string) *election {
	return &election{
		lockPath: filepath.Join(baseDir, "nats.lock"),
	}
}

// tryAcquire attempts a non-blocking flock. Returns true if the lock was acquired.
func (e *election) tryAcquire() (bool, error) {
	if e.lockFile != nil {
		return true, nil // already holding
	}

	if err := os.MkdirAll(filepath.Dir(e.lockPath), 0o755); err != nil {
		return false, fmt.Errorf("mkdir for lock: %w", err)
	}

	f, err := os.OpenFile(e.lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false, fmt.Errorf("open lock file: %w", err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return false, nil // lock held by another process
	}

	e.lockFile = f
	e.isLeader = true
	slog.Info("cluster: acquired leader lock")
	return true, nil
}

// acquireBlocking waits until the lock can be acquired (for ycode serve).
func (e *election) acquireBlocking() error {
	if e.lockFile != nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(e.lockPath), 0o755); err != nil {
		return fmt.Errorf("mkdir for lock: %w", err)
	}

	f, err := os.OpenFile(e.lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return fmt.Errorf("blocking flock: %w", err)
	}

	e.lockFile = f
	e.isLeader = true
	slog.Info("cluster: acquired leader lock (blocking)")
	return nil
}

// release unlocks and closes the lock file.
func (e *election) release() {
	if e.lockFile == nil {
		return
	}
	syscall.Flock(int(e.lockFile.Fd()), syscall.LOCK_UN)
	e.lockFile.Close()
	e.lockFile = nil
	e.isLeader = false
	slog.Info("cluster: released leader lock")
}

package fileops

import (
	"context"
	"path/filepath"
	"sync"
)

// FileLock provides per-file mutual exclusion for concurrent edit operations.
// When multiple subagents edit the same file, atomic writes prevent corruption
// but don't prevent lost updates. FileLock serializes access per file path.
//
// Inspired by opencode's semaphore-per-file locking for safe concurrent edits.
type FileLock struct {
	mu    sync.Mutex
	locks map[string]*entry
}

type entry struct {
	ch    chan struct{} // acts as a mutex (buffered size 1)
	count int           // number of waiters + holder
}

// NewFileLock creates a file lock registry.
func NewFileLock() *FileLock {
	return &FileLock{
		locks: make(map[string]*entry),
	}
}

// Lock acquires the lock for the given file path. Blocks until the lock is
// available or the context is cancelled. Returns nil on success, or ctx.Err()
// if the context was cancelled while waiting.
func (fl *FileLock) Lock(ctx context.Context, path string) error {
	key := fl.normalize(path)

	fl.mu.Lock()
	e, ok := fl.locks[key]
	if !ok {
		e = &entry{ch: make(chan struct{}, 1)}
		fl.locks[key] = e
	}
	e.count++
	fl.mu.Unlock()

	select {
	case e.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		fl.release(key)
		return ctx.Err()
	}
}

// Unlock releases the lock for the given file path.
// Panics if the lock is not held (programming error).
func (fl *FileLock) Unlock(path string) {
	key := fl.normalize(path)

	fl.mu.Lock()
	e, ok := fl.locks[key]
	if !ok {
		fl.mu.Unlock()
		panic("filelock: unlock of unlocked path: " + path)
	}
	fl.mu.Unlock()

	select {
	case <-e.ch:
		// Released.
	default:
		panic("filelock: unlock of unlocked path: " + path)
	}

	fl.release(key)
}

// release decrements the waiter count and cleans up if no one is waiting.
func (fl *FileLock) release(key string) {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	e, ok := fl.locks[key]
	if !ok {
		return
	}
	e.count--
	if e.count <= 0 {
		delete(fl.locks, key)
	}
}

// normalize returns a canonical key for a file path.
func (fl *FileLock) normalize(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

// ActiveLocks returns the number of file paths currently tracked.
// Useful for monitoring and diagnostics.
func (fl *FileLock) ActiveLocks() int {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	return len(fl.locks)
}

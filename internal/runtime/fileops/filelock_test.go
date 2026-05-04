package fileops

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestFileLock_BasicLockUnlock(t *testing.T) {
	fl := NewFileLock()

	err := fl.Lock(context.Background(), "/tmp/test.go")
	if err != nil {
		t.Fatal(err)
	}

	if fl.ActiveLocks() != 1 {
		t.Errorf("active = %d, want 1", fl.ActiveLocks())
	}

	fl.Unlock("/tmp/test.go")

	if fl.ActiveLocks() != 0 {
		t.Errorf("active = %d, want 0 after unlock", fl.ActiveLocks())
	}
}

func TestFileLock_ConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent lock test in short mode")
	}

	fl := NewFileLock()
	path := "/tmp/concurrent.go"

	var mu sync.Mutex
	counter := 0
	maxConcurrent := 0

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := fl.Lock(context.Background(), path)
			if err != nil {
				t.Errorf("lock failed: %v", err)
				return
			}

			mu.Lock()
			counter++
			if counter > maxConcurrent {
				maxConcurrent = counter
			}
			mu.Unlock()

			time.Sleep(5 * time.Millisecond) // simulate work

			mu.Lock()
			counter--
			mu.Unlock()

			fl.Unlock(path)
		}()
	}
	wg.Wait()

	if maxConcurrent > 1 {
		t.Errorf("max concurrent = %d, expected 1 (serialized)", maxConcurrent)
	}
}

func TestFileLock_DifferentPaths(t *testing.T) {
	fl := NewFileLock()

	err1 := fl.Lock(context.Background(), "/tmp/a.go")
	if err1 != nil {
		t.Fatal(err1)
	}
	err2 := fl.Lock(context.Background(), "/tmp/b.go")
	if err2 != nil {
		t.Fatal(err2)
	}

	if fl.ActiveLocks() != 2 {
		t.Errorf("active = %d, want 2", fl.ActiveLocks())
	}

	fl.Unlock("/tmp/a.go")
	fl.Unlock("/tmp/b.go")
}

func TestFileLock_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context cancellation test in short mode")
	}

	fl := NewFileLock()
	path := "/tmp/cancel.go"

	// Hold the lock.
	err := fl.Lock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	// Try to lock with a cancelled context.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = fl.Lock(ctx, path)
	if err == nil {
		t.Error("expected context cancellation error")
		fl.Unlock(path)
	}

	fl.Unlock(path)
}

func TestFileLock_UnlockPanics(t *testing.T) {
	fl := NewFileLock()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on unlock of unlocked path")
		}
	}()

	fl.Unlock("/tmp/not-locked.go")
}

func TestFileLock_NormalizesPath(t *testing.T) {
	fl := NewFileLock()

	// These should resolve to the same lock.
	err := fl.Lock(context.Background(), "/tmp/./test.go")
	if err != nil {
		t.Fatal(err)
	}

	if fl.ActiveLocks() != 1 {
		t.Errorf("active = %d, want 1", fl.ActiveLocks())
	}

	fl.Unlock("/tmp/test.go")
}

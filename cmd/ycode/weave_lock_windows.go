//go:build windows

package main

import "fmt"

// withWeaveQueueLock on Windows is best-effort: we simply load,
// mutate, save without an OS-level mutex. The MVP orchestrator
// flow targets unix; concurrent weave starts on Windows have
// undefined behavior pending a real LockFileEx implementation.
func withWeaveQueueLock(dir string, fn func(*weaveQueue) error) error {
	q, err := loadWeaveQueue(dir)
	if err != nil {
		return fmt.Errorf("queue lock: load: %w", err)
	}
	if err := fn(q); err != nil {
		return err
	}
	return saveWeaveQueue(dir, q)
}

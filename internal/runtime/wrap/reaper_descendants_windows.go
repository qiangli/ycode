//go:build windows

package wrap

// reapLeakedDescendants is a no-op on Windows. Process-group isolation
// and descendant reaping are Unix concepts; Windows process management
// is handled by its own job-object / process-group primitives.
func reapLeakedDescendants(_ int) {}

func reapLeakedPIDs(_ int, _ []int) {}

type leakedDescendantTracker struct{}

func startLeakedDescendantTracker(_ int) *leakedDescendantTracker {
	return &leakedDescendantTracker{}
}

func (t *leakedDescendantTracker) stopAndSnapshot() []int {
	return nil
}

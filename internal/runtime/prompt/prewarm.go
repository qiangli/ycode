package prompt

import (
	"context"
	"sync"

	"github.com/qiangli/ycode/internal/runtime/memory"
)

// PrewarmResult holds the results of concurrent startup discovery.
type PrewarmResult struct {
	ContextFiles []ContextFile
	Memories     []*memory.Memory
	Errors       []error
}

// Prewarm runs startup discovery tasks concurrently.
// It discovers instruction files and loads memories in parallel.
func Prewarm(ctx context.Context, workDir string, memManager *memory.Manager) *PrewarmResult {
	result := &PrewarmResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Task 1: Discover instruction files.
	wg.Add(1)
	go func() {
		defer wg.Done()
		files := DiscoverInstructionFiles(workDir)
		mu.Lock()
		result.ContextFiles = files
		mu.Unlock()
	}()

	// Task 2: Load all memories.
	if memManager != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mems, err := memManager.All()
			mu.Lock()
			if err != nil {
				result.Errors = append(result.Errors, err)
			} else {
				result.Memories = mems
			}
			mu.Unlock()
		}()
	}

	// Wait for all tasks (or context cancellation).
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		mu.Lock()
		result.Errors = append(result.Errors, ctx.Err())
		mu.Unlock()
	}

	return result
}

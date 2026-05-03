package session

import (
	"context"
	"sync"
	"time"
)

const (
	// PrefetchTimeout is the maximum time to wait for memory prefetch.
	// Tool execution typically takes longer, so this doesn't block.
	PrefetchTimeout = 5 * time.Second

	// PrefetchMaxResults is the maximum number of memories to prefetch.
	PrefetchMaxResults = 5
)

// MemoryResult represents a single retrieved memory for injection.
type MemoryResult struct {
	Path    string
	Content string
	Age     time.Duration
}

// MemorySearchFunc is the function signature for memory search operations.
// Implementations should search for memories relevant to the query and
// return up to maxResults items.
type MemorySearchFunc func(ctx context.Context, query string, maxResults int) ([]MemoryResult, error)

// MemoryPrefetch manages asynchronous memory retrieval during tool execution.
// It starts a search in the background when tools begin executing and collects
// results when tools complete — zero latency added to the critical path.
//
// Inspired by Claude Code's startRelevantMemoryPrefetch / MemoryPrefetch handle.
type MemoryPrefetch struct {
	mu      sync.Mutex
	results []MemoryResult
	err     error
	done    chan struct{}
}

// StartMemoryPrefetch begins an asynchronous memory search using the provided
// search function and query. The search runs in a goroutine with a timeout.
// Call Collect() to retrieve results after tool execution completes.
func StartMemoryPrefetch(searchFn MemorySearchFunc, query string) *MemoryPrefetch {
	mp := &MemoryPrefetch{
		done: make(chan struct{}),
	}

	go func() {
		defer close(mp.done)

		ctx, cancel := context.WithTimeout(context.Background(), PrefetchTimeout)
		defer cancel()

		results, err := searchFn(ctx, query, PrefetchMaxResults)

		mp.mu.Lock()
		mp.results = results
		mp.err = err
		mp.mu.Unlock()
	}()

	return mp
}

// Collect waits for the prefetch to complete and returns results.
// If the prefetch hasn't completed within the remaining timeout,
// it returns whatever is available (possibly nil).
func (mp *MemoryPrefetch) Collect(timeout time.Duration) ([]MemoryResult, error) {
	if timeout <= 0 {
		timeout = PrefetchTimeout
	}

	select {
	case <-mp.done:
		// Completed normally.
	case <-time.After(timeout):
		// Timeout — return what we have.
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()
	return mp.results, mp.err
}

// Done returns a channel that's closed when the prefetch completes.
func (mp *MemoryPrefetch) Done() <-chan struct{} {
	return mp.done
}

// DeduplicateMemories filters out memories that are already present in
// the conversation context. Uses path-based deduplication.
func DeduplicateMemories(results []MemoryResult, alreadySurfaced map[string]bool) []MemoryResult {
	if len(alreadySurfaced) == 0 {
		return results
	}

	var filtered []MemoryResult
	for _, r := range results {
		if !alreadySurfaced[r.Path] {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// FormatMemoryForInjection formats a memory result for injection into
// the conversation context. Includes staleness caveat for old memories.
func FormatMemoryForInjection(m MemoryResult) string {
	header := "Memory"
	if m.Age > 24*time.Hour {
		days := int(m.Age.Hours() / 24)
		header += " (saved " + formatAge(days) + ")"
	}
	header += ": " + m.Path + ":"

	result := header + "\n" + m.Content

	if m.Age > 24*time.Hour {
		result += "\n\nNote: This memory may be outdated. Verify before acting on it."
	}

	return result
}

// formatAge returns a human-readable age string.
func formatAge(days int) string {
	switch {
	case days == 1:
		return "1 day ago"
	case days < 30:
		return formatDays(days) + " days ago"
	case days < 365:
		months := days / 30
		if months == 1 {
			return "1 month ago"
		}
		return formatDays(months) + " months ago"
	default:
		years := days / 365
		if years == 1 {
			return "1 year ago"
		}
		return formatDays(years) + " years ago"
	}
}

func formatDays(n int) string {
	// Simple int to string without importing strconv for this trivial case.
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

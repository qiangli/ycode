package memory

import (
	"sort"
	"sync"
	"time"
)

// TimeBucket is a day-grained inverted index from YYYY-MM-DD to memory
// names. Two parallel indices are maintained: one keyed by CreatedAt and
// one by LastAccessedAt. Range queries return the union — a memory shows
// up in a window if it was created OR accessed within the window. This
// makes "what did we do this week" return things actively touched even
// when they predate the week.
//
// Day buckets are cheap memory-wise: at 10k memories the largest bucket
// is ~100 entries and the total index is <1MB. The index is rebuilt by
// the Dreamer and patched incrementally on Save/Forget.
type TimeBucket struct {
	mu         sync.RWMutex
	byCreated  map[string]map[string]struct{}
	byAccessed map[string]map[string]struct{}
}

// NewTimeBucket returns an empty index.
func NewTimeBucket() *TimeBucket {
	return &TimeBucket{
		byCreated:  make(map[string]map[string]struct{}),
		byAccessed: make(map[string]map[string]struct{}),
	}
}

// Rebuild replaces the index contents from the given memories. Safe to
// call concurrently with Range — readers will see either the old or new
// snapshot, never a half-built state.
func (tb *TimeBucket) Rebuild(memories []*Memory) {
	created := make(map[string]map[string]struct{})
	accessed := make(map[string]map[string]struct{})
	for _, mem := range memories {
		if mem == nil || mem.Name == "" {
			continue
		}
		if !mem.CreatedAt.IsZero() {
			addBucket(created, dayKey(mem.CreatedAt), mem.Name)
		}
		if !mem.LastAccessedAt.IsZero() {
			addBucket(accessed, dayKey(mem.LastAccessedAt), mem.Name)
		}
	}
	tb.mu.Lock()
	tb.byCreated = created
	tb.byAccessed = accessed
	tb.mu.Unlock()
}

// Add upserts a single memory into the index.
func (tb *TimeBucket) Add(mem *Memory) {
	if mem == nil || mem.Name == "" {
		return
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	if !mem.CreatedAt.IsZero() {
		addBucket(tb.byCreated, dayKey(mem.CreatedAt), mem.Name)
	}
	if !mem.LastAccessedAt.IsZero() {
		addBucket(tb.byAccessed, dayKey(mem.LastAccessedAt), mem.Name)
	}
}

// Remove drops all occurrences of name from both indices. Worst-case
// O(buckets); buckets are day-grained so the constant is small.
func (tb *TimeBucket) Remove(name string) {
	if name == "" {
		return
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	for key, set := range tb.byCreated {
		if _, ok := set[name]; ok {
			delete(set, name)
			if len(set) == 0 {
				delete(tb.byCreated, key)
			}
		}
	}
	for key, set := range tb.byAccessed {
		if _, ok := set[name]; ok {
			delete(set, name)
			if len(set) == 0 {
				delete(tb.byAccessed, key)
			}
		}
	}
}

// Range returns memory names whose CreatedAt OR LastAccessedAt falls in
// [start, end). Results are deduplicated and sorted lexicographically
// for stable output.
func (tb *TimeBucket) Range(start, end time.Time) []string {
	if !end.After(start) {
		return nil
	}
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	seen := make(map[string]struct{})
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		k := dayKey(day)
		for name := range tb.byCreated[k] {
			seen[name] = struct{}{}
		}
		for name := range tb.byAccessed[k] {
			seen[name] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// dayKey returns the YYYY-MM-DD bucket key for a time.
func dayKey(t time.Time) string {
	return t.Format("2006-01-02")
}

func addBucket(m map[string]map[string]struct{}, key, name string) {
	set, ok := m[key]
	if !ok {
		set = make(map[string]struct{})
		m[key] = set
	}
	set[name] = struct{}{}
}

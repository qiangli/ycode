package detector

import (
	"sync"
	"time"
)

// Dedupe keeps an in-memory TTL table of signatures the detector has
// already observed. The first sighting of a signature returns count=1
// and firstSeen=true; subsequent sightings within ttl increment count
// and return firstSeen=false so the caller can suppress duplicate
// JSONL writes without losing the rolling-window count.
//
// Phase 1: this is the only dedupe layer. Phase 2 will additionally
// check whether a backlog entry already exists on disk for the
// signature; Phase 4 adds a per-signature "in-flight fix attempt"
// guard against worker storms.
type Dedupe struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time // injected for tests
	m   map[string]*dedupeEntry
}

type dedupeEntry struct {
	firstSeen time.Time
	lastSeen  time.Time
	count     int
}

// NewDedupe returns a Dedupe with the given TTL. now may be nil to
// use time.Now (the production path).
func NewDedupe(ttl time.Duration, now func() time.Time) *Dedupe {
	if now == nil {
		now = time.Now
	}
	return &Dedupe{ttl: ttl, now: now, m: make(map[string]*dedupeEntry)}
}

// See records a sighting of signature. Returns whether this is the
// first sighting in the current TTL window and the running occurrence
// count for the window.
func (d *Dedupe) See(signature string) (firstSeen bool, count int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	d.evictLocked(now)
	if e, ok := d.m[signature]; ok {
		e.lastSeen = now
		e.count++
		return false, e.count
	}
	d.m[signature] = &dedupeEntry{firstSeen: now, lastSeen: now, count: 1}
	return true, 1
}

// evictLocked drops entries whose lastSeen is older than ttl. Cheap
// O(n) sweep — table size is bounded by the number of distinct
// signatures the detector has seen in the last ttl, which is small
// in practice (dozens, not millions).
func (d *Dedupe) evictLocked(now time.Time) {
	cutoff := now.Add(-d.ttl)
	for k, e := range d.m {
		if e.lastSeen.Before(cutoff) {
			delete(d.m, k)
		}
	}
}

// Size returns the current table size — used by ycode selfheal
// status in Phase 4 and by tests now.
func (d *Dedupe) Size() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.m)
}

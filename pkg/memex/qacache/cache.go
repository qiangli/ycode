package qacache

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/qiangli/ycode/pkg/memex/store/fileatomic"
)

// Entry is a single cached question→answer pair plus the metadata the
// promotion path needs (how often it's been asked, when, by whom).
type Entry struct {
	Key         string        `json:"key"`
	Canonical   string        `json:"canonical"`
	Question    string        `json:"question"` // original surface form
	Answer      string        `json:"answer"`
	Class       QuestionClass `json:"class"`
	TTL         time.Duration `json:"ttl_nanoseconds"`
	CreatedAt   time.Time     `json:"created_at"`
	LastAskedAt time.Time     `json:"last_asked_at"`
	AskCount    int           `json:"ask_count"`
	// Entities lifts the entity names mentioned in the answer so the
	// memex Manager can invalidate this entry when one of those entities
	// is touched. Populated by the caller after extraction.
	Entities []string `json:"entities,omitempty"`
	// Sources records the tool calls or memory names that produced the
	// answer (e.g. "git_log", "memex:auth-decision"). Lets the agent
	// re-derive if the user asks for fresh data.
	Sources []string `json:"sources,omitempty"`
}

// Cache is the in-memory + on-disk Q→A store. One Cache instance per
// scope dir (per-user, per-project).
type Cache struct {
	mu      sync.RWMutex
	dir     string
	entries map[string]*Entry

	// Stats are read by the /qacache stats builtin.
	hits        int
	misses      int
	writes      int
	invalidated int
	promoted    int
}

// New returns a Cache rooted at dir. The directory is created if it does
// not exist. When dir is empty, the cache runs in-memory only — useful
// for tests and ephemeral sessions.
func New(dir string) (*Cache, error) {
	c := &Cache{
		dir:     dir,
		entries: make(map[string]*Entry),
	}
	if dir == "" {
		return c, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create qacache dir: %w", err)
	}
	if err := c.loadAll(); err != nil {
		return nil, fmt.Errorf("load qacache: %w", err)
	}
	return c, nil
}

// loadAll reads every entry file from disk into the in-memory map. Bad
// entries are skipped with a warning rather than failing startup.
func (c *Cache) loadAll() error {
	files, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.dir, f.Name()))
		if err != nil {
			slog.Warn("qacache: read entry failed", "file", f.Name(), "error", err)
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			slog.Warn("qacache: parse entry failed", "file", f.Name(), "error", err)
			continue
		}
		if e.Key == "" {
			continue
		}
		c.entries[e.Key] = &e
	}
	return nil
}

// Lookup returns the entry matching the normalized question, or nil
// when the cache misses. A matched-but-expired entry is removed before
// returning miss.
//
// When `now` falls within ±1 day of an existing entry's date tokens but
// the exact key differs (e.g., "today" asked on two adjacent calendar
// days), the lookup tries the adjacent date variants before giving up.
// This stops false-misses across midnight while keeping the day-granular
// freshness story.
func (c *Cache) Lookup(question string, now time.Time) *Entry {
	c.mu.Lock()
	defer c.mu.Unlock()

	primary := Normalize(question, now)
	if e := c.lookupKey(primary.Key, now); e != nil {
		c.hits++
		return e
	}

	// ±1-day fuzzy: try with now shifted by 1 day on either side. Only
	// useful when the question carries a relative-time token; otherwise
	// the key is independent of `now` and shifting changes nothing.
	if len(primary.DateTokens) > 0 {
		for _, delta := range []int{-1, 1} {
			alt := Normalize(question, now.AddDate(0, 0, delta))
			if alt.Key == primary.Key {
				continue
			}
			if e := c.lookupKey(alt.Key, now); e != nil {
				c.hits++
				return e
			}
		}
	}
	c.misses++
	return nil
}

// lookupKey returns a live entry by exact key or nil (purging if expired).
// Must be called with mu held.
func (c *Cache) lookupKey(key string, now time.Time) *Entry {
	e, ok := c.entries[key]
	if !ok {
		return nil
	}
	if !e.CreatedAt.IsZero() && e.TTL > 0 && now.Sub(e.CreatedAt) > e.TTL {
		// Expired — drop quietly.
		delete(c.entries, key)
		c.removeFromDisk(key)
		return nil
	}
	e.AskCount++
	e.LastAskedAt = now
	c.persist(e)
	return e
}

// Record inserts or updates an entry for the question/answer pair. The
// class controls TTL; entities/sources are stored verbatim for later
// invalidation and re-derivation.
func (c *Cache) Record(question, answer string, now time.Time, entities, sources []string) *Entry {
	c.mu.Lock()
	defer c.mu.Unlock()

	norm := Normalize(question, now)
	class := Classify(question)
	e := &Entry{
		Key:         norm.Key,
		Canonical:   norm.Canonical,
		Question:    question,
		Answer:      answer,
		Class:       class,
		TTL:         class.TTL(),
		CreatedAt:   now,
		LastAskedAt: now,
		AskCount:    1,
		Entities:    dedupStrings(entities),
		Sources:     dedupStrings(sources),
	}
	if prev, ok := c.entries[norm.Key]; ok {
		// Preserve ask count across rewrites; treat re-record as an
		// update of the cached answer (not a new question).
		e.AskCount = prev.AskCount + 1
		if e.CreatedAt.IsZero() {
			e.CreatedAt = prev.CreatedAt
		}
	}
	c.entries[norm.Key] = e
	c.writes++
	c.persist(e)
	return e
}

// InvalidateByEntities drops any entry whose Entities set intersects the
// given list. Returns the count of removed entries. Used by the memex
// Manager: when a Save/Forget touches an entity, related cached answers
// should be re-derived next ask.
func (c *Cache) InvalidateByEntities(entities []string) int {
	if len(entities) == 0 {
		return 0
	}
	wanted := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		if e != "" {
			wanted[e] = struct{}{}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for key, e := range c.entries {
		for _, ent := range e.Entities {
			if _, ok := wanted[ent]; ok {
				delete(c.entries, key)
				c.removeFromDisk(key)
				n++
				break
			}
		}
	}
	c.invalidated += n
	return n
}

// PromotionCandidates returns entries eligible for promotion to a
// memex memory: asked ≥2× and surviving ≥1 day. Sorted by AskCount
// descending so the most-asked questions get promoted first.
func (c *Cache) PromotionCandidates(now time.Time) []*Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []*Entry
	for _, e := range c.entries {
		if e.AskCount < 2 {
			continue
		}
		if now.Sub(e.CreatedAt) < 24*time.Hour {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].AskCount > out[j].AskCount
	})
	return out
}

// MarkPromoted removes a promoted entry. Idempotent — unknown keys are
// silently dropped. Bumps the promoted counter for stats.
func (c *Cache) MarkPromoted(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; !ok {
		return
	}
	delete(c.entries, key)
	c.removeFromDisk(key)
	c.promoted++
}

// Stats is a snapshot of cache counters for the /qacache stats builtin.
type Stats struct {
	Entries     int
	Hits        int
	Misses      int
	Writes      int
	Invalidated int
	Promoted    int
}

// Stats returns a snapshot of the cache counters.
func (c *Cache) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Stats{
		Entries:     len(c.entries),
		Hits:        c.hits,
		Misses:      c.misses,
		Writes:      c.writes,
		Invalidated: c.invalidated,
		Promoted:    c.promoted,
	}
}

// persist writes the entry to disk. Caller holds mu.
func (c *Cache) persist(e *Entry) {
	if c.dir == "" {
		return
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		slog.Warn("qacache: marshal failed", "key", e.Key, "error", err)
		return
	}
	path := filepath.Join(c.dir, e.Key+".json")
	if err := fileatomic.AtomicWriteFile(path, data, 0o600); err != nil {
		slog.Warn("qacache: write failed", "key", e.Key, "error", err)
	}
}

// removeFromDisk deletes the entry file. Caller holds mu.
func (c *Cache) removeFromDisk(key string) {
	if c.dir == "" {
		return
	}
	path := filepath.Join(c.dir, key+".json")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("qacache: remove failed", "key", key, "error", err)
	}
}

func dedupStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

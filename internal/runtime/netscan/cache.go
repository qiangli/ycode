package netscan

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Cache persists discovered hosts to disk so subsequent /netscan runs
// can render the most-recently-seen list immediately, even before a
// fresh discovery pass completes.
//
// Storage: a single JSON file. No SQLite, no churn — netscan runs are
// rare enough (one or twice an interactive session) that file-rewrites
// don't matter, and a flat file lets users grep / hand-edit when
// debugging stale entries.
type Cache struct {
	path string
	mu   sync.Mutex
}

// CacheRecord is the wire shape of a single host on disk; mirrors Host
// minus transient Sources (which are reset to SourceCache on load) so
// the file remains stable across format tweaks.
type CacheRecord struct {
	Name      string            `json:"name,omitempty"`
	IP        string            `json:"ip"`
	Port      int               `json:"port,omitempty"`
	Service   string            `json:"service,omitempty"`
	SeenCount int               `json:"seen_count,omitempty"`
	LastSeen  time.Time         `json:"last_seen"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// NewCache returns a Cache writing to path. Pass empty for the
// default ~/.agents/ycode/netscan-cache.json.
func NewCache(path string) *Cache {
	if path == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, ".agents", "ycode", "netscan-cache.json")
		}
	}
	return &Cache{path: path}
}

// Load reads the cache and returns the stored hosts as a Host slice
// stamped with SourceCache. Missing file is not an error — returns
// an empty slice.
func (c *Cache) Load() ([]Host, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []CacheRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, err
	}
	hosts := make([]Host, 0, len(recs))
	for _, r := range recs {
		hosts = append(hosts, Host{
			Name:      r.Name,
			IP:        r.IP,
			Port:      r.Port,
			Service:   r.Service,
			Sources:   []Source{SourceCache},
			SeenCount: r.SeenCount,
			LastSeen:  r.LastSeen,
			Attrs:     r.Attrs,
		})
	}
	return hosts, nil
}

// Save persists hosts to disk, advancing SeenCount for repeat
// observations and dropping entries older than maxAge (zero means
// keep forever). Idempotent and safe under concurrent callers.
func (c *Cache) Save(hosts []Host, maxAge time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}

	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().UTC().Add(-maxAge)
	}
	recs := make([]CacheRecord, 0, len(hosts))
	for _, h := range hosts {
		if !cutoff.IsZero() && h.LastSeen.Before(cutoff) {
			continue
		}
		recs = append(recs, CacheRecord{
			Name:      h.Name,
			IP:        h.IP,
			Port:      h.Port,
			Service:   h.Service,
			SeenCount: h.SeenCount,
			LastSeen:  h.LastSeen,
			Attrs:     h.Attrs,
		})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].LastSeen.After(recs[j].LastSeen) })

	tmp := c.path + ".tmp"
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// MergeAndStamp folds a fresh discovery pass into a cache-loaded set,
// bumps SeenCount on rows that were re-observed, and returns the
// merged slice. This is the "what to display" view; pair with Save to
// persist the same shape.
func MergeAndStamp(cached []Host, fresh []Host) []Host {
	freshIPs := make(map[string]bool, len(fresh))
	for _, h := range fresh {
		freshIPs[h.IP] = true
	}
	merged := Merge(cached, fresh)
	for i := range merged {
		if freshIPs[merged[i].IP] {
			merged[i].SeenCount++
		}
	}
	return merged
}

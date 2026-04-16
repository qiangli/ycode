package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CompletionCache stores full LLM responses keyed by request hash.
// When an identical request is made within the TTL window, the cached
// response is returned immediately — zero tokens, zero latency.
// This is useful for retries, error recovery loops, and rapid re-sends
// where the request hasn't changed.
type CompletionCache struct {
	mu  sync.Mutex
	dir string // disk storage directory
	ttl time.Duration

	// In-memory index: request hash → cached entry.
	entries map[string]*completionEntry

	// Stats
	Hits   int
	Misses int
	Writes int
}

type completionEntry struct {
	Response  *Response `json:"response"`
	CachedAt  time.Time `json:"cached_at"`
	RequestFP string    `json:"request_fingerprint"`
}

// NewCompletionCache creates a completion cache backed by the given directory.
// If dir is empty, the cache operates in memory only.
func NewCompletionCache(dir string, ttl time.Duration) *CompletionCache {
	if ttl <= 0 {
		ttl = CompletionCacheTTL
	}
	cc := &CompletionCache{
		dir:     dir,
		ttl:     ttl,
		entries: make(map[string]*completionEntry),
	}

	// Ensure directory exists.
	if dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	return cc
}

// RequestHash computes a deterministic hash for a request using the existing
// fingerprint infrastructure.
func RequestHash(req *Request) string {
	fp := Fingerprint(req)
	// Combine all component hashes into a single key.
	combined := fp.ModelHash + ":" + fp.SystemHash + ":" + fp.ToolsHash + ":" + fp.MessagesHash
	return hashString(combined)
}

// Lookup checks for a cached completion matching the request hash.
// Returns nil if not found or expired.
func (cc *CompletionCache) Lookup(hash string) *Response {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	// Check in-memory first.
	if entry, ok := cc.entries[hash]; ok {
		if time.Since(entry.CachedAt) <= cc.ttl {
			cc.Hits++
			slog.Debug("completion cache hit", "hash", truncHash(hash))
			return entry.Response
		}
		// Expired — remove.
		delete(cc.entries, hash)
	}

	// Try disk.
	if cc.dir != "" {
		entry := cc.loadFromDisk(hash)
		if entry != nil && time.Since(entry.CachedAt) <= cc.ttl {
			cc.entries[hash] = entry
			cc.Hits++
			slog.Debug("completion cache hit (disk)", "hash", truncHash(hash))
			return entry.Response
		}
	}

	cc.Misses++
	return nil
}

// Store saves a completion response for the given request hash.
func (cc *CompletionCache) Store(hash string, resp *Response) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	entry := &completionEntry{
		Response:  resp,
		CachedAt:  time.Now(),
		RequestFP: hash,
	}
	cc.entries[hash] = entry
	cc.Writes++

	// Persist to disk asynchronously.
	if cc.dir != "" {
		go cc.saveToDisk(hash, entry)
	}
}

// Clear removes all cached entries (e.g., after compaction changes context).
func (cc *CompletionCache) Clear() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.entries = make(map[string]*completionEntry)

	// Clean disk cache.
	if cc.dir != "" {
		entries, _ := os.ReadDir(cc.dir)
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".json" {
				_ = os.Remove(filepath.Join(cc.dir, e.Name()))
			}
		}
	}
}

// Stats returns a copy of the cache statistics.
func (cc *CompletionCache) Stats() (hits, misses, writes int) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.Hits, cc.Misses, cc.Writes
}

// truncHash safely truncates a hash for display/filenames.
func truncHash(hash string) string {
	if len(hash) > 16 {
		return hash[:16]
	}
	return hash
}

func (cc *CompletionCache) saveToDisk(hash string, entry *completionEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := filepath.Join(cc.dir, fmt.Sprintf("%s.json", truncHash(hash)))
	_ = os.WriteFile(path, data, 0o600)
}

func (cc *CompletionCache) loadFromDisk(hash string) *completionEntry {
	path := filepath.Join(cc.dir, fmt.Sprintf("%s.json", truncHash(hash)))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entry completionEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	return &entry
}

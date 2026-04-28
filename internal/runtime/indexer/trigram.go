package indexer

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/qiangli/ycode/internal/storage"
)

const trigramBucket = "trigrams"

// TrigramIndex provides trigram-based file lookup for accelerating regex search.
// Each file is decomposed into 3-character substrings (trigrams), and queries
// extract trigrams from the search pattern to intersect candidate file sets.
type TrigramIndex struct {
	mu sync.RWMutex
	kv storage.KVStore

	// In-memory cache for fast lookups during a session.
	// Trigram -> set of file paths.
	cache map[string]map[string]bool
}

// NewTrigramIndex creates a trigram index backed by a KV store.
func NewTrigramIndex(kv storage.KVStore) *TrigramIndex {
	if kv == nil {
		return nil
	}
	return &TrigramIndex{
		kv:    kv,
		cache: make(map[string]map[string]bool),
	}
}

// IndexFile extracts trigrams from a file and records them.
func (ti *TrigramIndex) IndexFile(absPath, relPath string) {
	if ti == nil {
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return
	}

	content := string(data)
	trigrams := extractTrigrams(content)

	ti.mu.Lock()
	defer ti.mu.Unlock()

	for _, tri := range trigrams {
		if ti.cache[tri] == nil {
			ti.cache[tri] = make(map[string]bool)
		}
		ti.cache[tri][relPath] = true
	}
}

// Query returns file paths that contain all the given trigrams.
// This is used to narrow candidate files before running a regex scan.
func (ti *TrigramIndex) Query(trigrams []string) []string {
	if ti == nil || len(trigrams) == 0 {
		return nil
	}

	ti.mu.RLock()
	defer ti.mu.RUnlock()

	// Start with the first trigram's file set.
	var result map[string]bool
	for _, tri := range trigrams {
		files := ti.cache[tri]
		if files == nil {
			// If any trigram has no files, intersection is empty.
			return nil
		}
		if result == nil {
			result = make(map[string]bool, len(files))
			for f := range files {
				result[f] = true
			}
		} else {
			// Intersect.
			for f := range result {
				if !files[f] {
					delete(result, f)
				}
			}
		}
		if len(result) == 0 {
			return nil
		}
	}

	paths := make([]string, 0, len(result))
	for f := range result {
		paths = append(paths, f)
	}
	return paths
}

// QueryPattern extracts trigrams from a search string and returns matching files.
func (ti *TrigramIndex) QueryPattern(pattern string) []string {
	if ti == nil {
		return nil
	}
	trigrams := extractTrigrams(pattern)
	if len(trigrams) == 0 {
		return nil
	}
	return ti.Query(trigrams)
}

// Save persists the in-memory trigram index to the KV store.
func (ti *TrigramIndex) Save() error {
	if ti == nil {
		return nil
	}

	ti.mu.RLock()
	defer ti.mu.RUnlock()

	for tri, files := range ti.cache {
		fileList := make([]string, 0, len(files))
		for f := range files {
			fileList = append(fileList, f)
		}
		data, err := json.Marshal(fileList)
		if err != nil {
			continue
		}
		_ = ti.kv.Put(trigramBucket, tri, data)
	}
	return nil
}

// Load restores the trigram index from the KV store.
func (ti *TrigramIndex) Load() error {
	if ti == nil {
		return nil
	}

	ti.mu.Lock()
	defer ti.mu.Unlock()

	return ti.kv.ForEach(trigramBucket, func(key string, value []byte) error {
		var files []string
		if err := json.Unmarshal(value, &files); err != nil {
			return nil
		}
		fileSet := make(map[string]bool, len(files))
		for _, f := range files {
			fileSet[f] = true
		}
		ti.cache[key] = fileSet
		return nil
	})
}

// Stats returns the number of unique trigrams and total file entries.
func (ti *TrigramIndex) Stats() (trigrams, entries int) {
	if ti == nil {
		return 0, 0
	}
	ti.mu.RLock()
	defer ti.mu.RUnlock()
	trigrams = len(ti.cache)
	for _, files := range ti.cache {
		entries += len(files)
	}
	return
}

// extractTrigrams returns unique 3-character substrings from the given text.
// Trigrams are extracted from each line independently (no cross-line trigrams).
func extractTrigrams(text string) []string {
	seen := make(map[string]bool)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 3 {
			continue
		}
		for i := 0; i <= len(line)-3; i++ {
			tri := line[i : i+3]
			// Skip trigrams with only whitespace.
			if strings.TrimSpace(tri) == "" {
				continue
			}
			seen[tri] = true
		}
	}

	result := make([]string, 0, len(seen))
	for tri := range seen {
		result = append(result, tri)
	}
	return result
}

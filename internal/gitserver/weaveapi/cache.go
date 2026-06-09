package weaveapi

import "sync"

// labelCache memoizes label-name → ID lookups per (owner, repo). Used
// by ops.go to avoid an extra ListLabels round-trip on every label
// operation. Race-safe; size bound is the small loom-owned label set
// (~16 entries per repo) so no eviction.
type labelCache struct {
	mu sync.Mutex
	m  map[string]int64
}

func newLabelCache() *labelCache {
	return &labelCache{m: map[string]int64{}}
}

func (c *labelCache) put(owner, repo, name string, id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key(owner, repo, name)] = id
}

func (c *labelCache) get(owner, repo, name string) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, ok := c.m[key(owner, repo, name)]
	return id, ok
}

func key(owner, repo, name string) string { return owner + "/" + repo + "#" + name }

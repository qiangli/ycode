package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const (
	// CompletionCacheTTL is the cache TTL for completions.
	CompletionCacheTTL = 30 * time.Second
	// PromptCacheTTL is the cache TTL for prompt fingerprints.
	PromptCacheTTL = 5 * time.Minute
	// CacheBreakThreshold is the token drop that signals an unexpected cache break.
	CacheBreakThreshold = 2000
)

// PromptFingerprint holds hashes of prompt components.
type PromptFingerprint struct {
	ModelHash    string `json:"model_hash"`
	SystemHash   string `json:"system_hash"`
	ToolsHash    string `json:"tools_hash"`
	MessagesHash string `json:"messages_hash"`
}

// PromptCache tracks prompt fingerprints and cache statistics.
type PromptCache struct {
	mu sync.Mutex

	lastFingerprint     *PromptFingerprint
	lastFingerprintAt   time.Time
	lastCacheReadTokens int

	// Stats
	Hits   int
	Misses int
	Writes int
	Breaks int
}

// NewPromptCache creates a new prompt cache.
func NewPromptCache() *PromptCache {
	return &PromptCache{}
}

// Fingerprint computes a fingerprint for a request.
func Fingerprint(req *Request) *PromptFingerprint {
	return &PromptFingerprint{
		ModelHash:    hashString(req.Model),
		SystemHash:   hashString(req.System),
		ToolsHash:    hashJSON(req.Tools),
		MessagesHash: hashJSON(req.Messages),
	}
}

// Check compares a fingerprint with the cached one.
func (pc *PromptCache) Check(fp *PromptFingerprint) bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.lastFingerprint == nil {
		pc.Misses++
		return false
	}

	if time.Since(pc.lastFingerprintAt) > PromptCacheTTL {
		pc.Misses++
		return false
	}

	// Check if static parts match (model + system + tools).
	if fp.ModelHash == pc.lastFingerprint.ModelHash &&
		fp.SystemHash == pc.lastFingerprint.SystemHash &&
		fp.ToolsHash == pc.lastFingerprint.ToolsHash {
		pc.Hits++
		return true
	}

	pc.Misses++
	return false
}

// Update stores a new fingerprint.
func (pc *PromptCache) Update(fp *PromptFingerprint) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.lastFingerprint = fp
	pc.lastFingerprintAt = time.Now()
	pc.Writes++
}

// DetectBreak checks for unexpected cache breaks based on token count drops.
func (pc *PromptCache) DetectBreak(cacheReadTokens int) bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.lastCacheReadTokens > 0 {
		drop := pc.lastCacheReadTokens - cacheReadTokens
		if drop > CacheBreakThreshold {
			pc.Breaks++
			pc.lastCacheReadTokens = cacheReadTokens
			return true
		}
	}

	pc.lastCacheReadTokens = cacheReadTokens
	return false
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}

func hashJSON(v any) string {
	data, _ := json.Marshal(v)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

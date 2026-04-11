package prompt

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

// ContextBaseline tracks per-section content hashes from the previous turn.
// When prompt caching is unavailable, unchanged sections can be omitted to
// reduce input tokens.
type ContextBaseline struct {
	mu         sync.Mutex
	hashes     map[string]string // section name → content hash
	turnNumber int
}

// NewContextBaseline creates an empty baseline (first turn sends everything).
func NewContextBaseline() *ContextBaseline {
	return &ContextBaseline{
		hashes: make(map[string]string),
	}
}

// DiffResult describes which sections changed relative to the baseline.
type DiffResult struct {
	Changed   []string // section names whose content changed
	Unchanged []string // section names whose content is identical
	IsFirst   bool     // true if no baseline exists yet (send everything)
}

// Diff compares current section contents against the stored baseline.
// Returns which sections changed and which are unchanged.
func (b *ContextBaseline) Diff(current map[string]string) DiffResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	result := DiffResult{
		IsFirst: b.turnNumber == 0,
	}

	for name, content := range current {
		hash := hashContent(content)
		if prevHash, ok := b.hashes[name]; ok && prevHash == hash {
			result.Unchanged = append(result.Unchanged, name)
		} else {
			result.Changed = append(result.Changed, name)
		}
	}

	return result
}

// Update stores the current section hashes as the new baseline.
// Call this after a successful API request.
func (b *ContextBaseline) Update(current map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.hashes = make(map[string]string, len(current))
	for name, content := range current {
		b.hashes[name] = hashContent(content)
	}
	b.turnNumber++
}

// Reset clears the baseline, forcing the next turn to send everything.
// Call this after compaction or emergency flush.
func (b *ContextBaseline) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.hashes = make(map[string]string)
	b.turnNumber = 0
}

// TurnNumber returns the number of successful updates.
func (b *ContextBaseline) TurnNumber() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.turnNumber
}

func hashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}

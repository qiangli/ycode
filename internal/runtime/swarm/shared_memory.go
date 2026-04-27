package swarm

import (
	"strings"
	"sync"
)

// SharedMemoryView provides a scoped, read-only view of memories for agents in a swarm.
// Agents can read pre-filtered relevant memories and observe new memories from siblings.
type SharedMemoryView struct {
	mu       sync.RWMutex
	entries  []SharedMemoryEntry
	maxItems int
}

// SharedMemoryEntry represents a single memory visible to swarm agents.
type SharedMemoryEntry struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Source  string `json:"source"` // agent ID that created it
}

// NewSharedMemoryView creates a shared memory view with the given capacity.
func NewSharedMemoryView(maxItems int) *SharedMemoryView {
	if maxItems <= 0 {
		maxItems = 50
	}
	return &SharedMemoryView{
		maxItems: maxItems,
	}
}

// Add adds a memory entry visible to all swarm agents.
func (sv *SharedMemoryView) Add(entry SharedMemoryEntry) {
	sv.mu.Lock()
	defer sv.mu.Unlock()

	// Deduplicate by name.
	for i, e := range sv.entries {
		if e.Name == entry.Name {
			sv.entries[i] = entry
			return
		}
	}

	sv.entries = append(sv.entries, entry)

	// Evict oldest if over capacity.
	if len(sv.entries) > sv.maxItems {
		sv.entries = sv.entries[1:]
	}
}

// All returns all shared memory entries.
func (sv *SharedMemoryView) All() []SharedMemoryEntry {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	result := make([]SharedMemoryEntry, len(sv.entries))
	copy(result, sv.entries)
	return result
}

// Search returns entries matching the query (simple keyword match).
func (sv *SharedMemoryView) Search(query string) []SharedMemoryEntry {
	sv.mu.RLock()
	defer sv.mu.RUnlock()

	query = strings.ToLower(query)
	var results []SharedMemoryEntry
	for _, e := range sv.entries {
		if strings.Contains(strings.ToLower(e.Content), query) ||
			strings.Contains(strings.ToLower(e.Name), query) {
			results = append(results, e)
		}
	}
	return results
}

// FormatForPrompt renders shared memories as a string for system prompt injection.
func (sv *SharedMemoryView) FormatForPrompt() string {
	sv.mu.RLock()
	defer sv.mu.RUnlock()

	if len(sv.entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Shared Swarm Memory\n\n")
	for _, e := range sv.entries {
		sb.WriteString("### ")
		sb.WriteString(e.Name)
		if e.Source != "" {
			sb.WriteString(" (from: ")
			sb.WriteString(e.Source)
			sb.WriteString(")")
		}
		sb.WriteString("\n")
		sb.WriteString(e.Content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// Len returns the number of entries.
func (sv *SharedMemoryView) Len() int {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return len(sv.entries)
}

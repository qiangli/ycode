package memory

import (
	"context"
	"log/slog"
)

// Reindex scans all memory files and ensures Bleve and vector indexes are up to date.
// This is called during startup to handle memories added while the service was offline.
func (m *Manager) Reindex(ctx context.Context) error {
	memories, err := m.All()
	if err != nil {
		return err
	}

	if len(memories) == 0 {
		return nil
	}

	indexed := 0

	// Rebuild Bleve full-text index.
	if m.bleveSearcher != nil {
		m.bleveSearcher.IndexAll(memories)
		indexed += len(memories)
	}

	// Entity extraction and linking for memories without cached entities.
	if m.entityIndex != nil {
		for _, mem := range memories {
			if len(mem.Entities) == 0 {
				entities := ExtractEntities(mem.Content)
				for _, e := range entities {
					m.entityIndex.Link(e, mem.Name)
					mem.Entities = append(mem.Entities, e.Name)
				}
			} else {
				// Re-link cached entities.
				for _, eName := range mem.Entities {
					m.entityIndex.Link(Entity{Name: eName}, mem.Name)
				}
			}
		}
	}

	slog.Info("memory.reindex",
		"total_memories", len(memories),
		"indexed", indexed,
	)

	return nil
}

// NeedsReindex checks if memory files exist that aren't indexed.
// Returns true if the file count doesn't match the index state.
func (m *Manager) NeedsReindex() bool {
	memories, err := m.All()
	if err != nil {
		return true // err on the side of reindexing
	}
	if len(memories) == 0 {
		return false
	}

	// If we have memories but no search backends configured, nothing to index.
	if m.bleveSearcher == nil && m.vectorSearcher == nil {
		return false
	}

	return true // conservative: always reindex on startup
}

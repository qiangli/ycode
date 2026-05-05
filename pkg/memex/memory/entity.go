package memory

import (
	"regexp"
	"strings"
	"time"
)

// Entity represents a named entity extracted from memory content.
type Entity struct {
	Name       string    `json:"name"`
	Type       string    `json:"type"` // person, project, technology, organization, concept, file_path
	Aliases    []string  `json:"aliases,omitempty"`
	MemoryRefs []string  `json:"memory_refs,omitempty"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

// EntityIndex provides entity-based memory linking and retrieval.
type EntityIndex struct {
	// entities maps entity name (lowercased) → Entity.
	entities map[string]*Entity
}

// NewEntityIndex creates a new in-memory entity index.
func NewEntityIndex() *EntityIndex {
	return &EntityIndex{
		entities: make(map[string]*Entity),
	}
}

// SetEntityIndex attaches an entity index to the manager.
func (m *Manager) SetEntityIndex(ei *EntityIndex) {
	m.entityIndex = ei
}

// Link associates an entity with a memory by name.
func (ei *EntityIndex) Link(entity Entity, memoryName string) {
	key := strings.ToLower(entity.Name)
	if existing, ok := ei.entities[key]; ok {
		// Update existing entity.
		existing.LastSeen = time.Now()
		for _, ref := range existing.MemoryRefs {
			if ref == memoryName {
				return
			}
		}
		existing.MemoryRefs = append(existing.MemoryRefs, memoryName)
		return
	}
	entity.MemoryRefs = append(entity.MemoryRefs, memoryName)
	if entity.FirstSeen.IsZero() {
		entity.FirstSeen = time.Now()
	}
	entity.LastSeen = time.Now()
	ei.entities[key] = &entity
}

// Unlink removes all entity associations for a memory.
func (ei *EntityIndex) Unlink(memoryName string) {
	for key, entity := range ei.entities {
		refs := entity.MemoryRefs[:0]
		for _, ref := range entity.MemoryRefs {
			if ref != memoryName {
				refs = append(refs, ref)
			}
		}
		entity.MemoryRefs = refs
		if len(entity.MemoryRefs) == 0 {
			delete(ei.entities, key)
		}
	}
}

// FindMemories returns memory names linked to an entity.
func (ei *EntityIndex) FindMemories(entityName string) []string {
	key := strings.ToLower(entityName)
	if entity, ok := ei.entities[key]; ok {
		return entity.MemoryRefs
	}
	return nil
}

// FindRelated returns other memories that share entities with the given memory.
func (ei *EntityIndex) FindRelated(memoryName string) []string {
	seen := make(map[string]int) // memory name → shared entity count
	for _, entity := range ei.entities {
		hasTarget := false
		for _, ref := range entity.MemoryRefs {
			if ref == memoryName {
				hasTarget = true
				break
			}
		}
		if hasTarget {
			for _, ref := range entity.MemoryRefs {
				if ref != memoryName {
					seen[ref]++
				}
			}
		}
	}

	type scored struct {
		name  string
		count int
	}
	var results []scored
	for name, count := range seen {
		results = append(results, scored{name, count})
	}
	// Sort by shared entity count descending.
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].count > results[i].count {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.name
	}
	return names
}

// SearchMemories finds memories linked to entities matching query terms.
// Returns SearchResults compatible with RRF fusion.
func (ei *EntityIndex) SearchMemories(query string, memByName map[string]*Memory, maxResults int) []SearchResult {
	queryTerms := strings.Fields(strings.ToLower(query))
	if len(queryTerms) == 0 {
		return nil
	}

	// Score memory names by how many entity matches they have.
	memScores := make(map[string]float64)
	for key, entity := range ei.entities {
		matched := false
		for _, term := range queryTerms {
			if strings.Contains(key, term) {
				matched = true
				break
			}
		}
		if matched {
			for _, ref := range entity.MemoryRefs {
				memScores[ref] += 1.0
			}
		}
	}

	var results []SearchResult
	for name, score := range memScores {
		mem, ok := memByName[name]
		if !ok {
			continue
		}
		results = append(results, SearchResult{
			Memory: mem,
			Score:  score,
			Source: "entity",
		})
	}

	// Sort by score descending.
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

// ExtractEntities extracts named entities from content using rule-based patterns.
// Conservative: prefers precision over recall to avoid false positives.
func ExtractEntities(content string) []Entity {
	var entities []Entity
	seen := make(map[string]bool)

	// File paths: /foo/bar.go, ./src/main.ts, internal/pkg/file.go
	filePathRe := regexp.MustCompile(`(?:^|[\s(])(/[\w./-]+\.(?:go|ts|js|py|rs|md|yaml|yml|json|toml)|\.?/[\w./-]+\.(?:go|ts|js|py|rs|md|yaml|yml|json|toml))`)
	for _, match := range filePathRe.FindAllStringSubmatch(content, -1) {
		path := strings.TrimSpace(match[1])
		if !seen[path] {
			seen[path] = true
			entities = append(entities, Entity{Name: path, Type: "file_path"})
		}
	}

	// Go package paths: github.com/foo/bar, internal/pkg/name
	goPkgRe := regexp.MustCompile(`(?:^|[\s"])([a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)+/[\w./-]+)`)
	for _, match := range goPkgRe.FindAllStringSubmatch(content, -1) {
		pkg := match[1]
		if !seen[pkg] {
			seen[pkg] = true
			entities = append(entities, Entity{Name: pkg, Type: "technology"})
		}
	}

	// URLs: http(s)://...
	urlRe := regexp.MustCompile(`https?://[^\s)>"]+`)
	for _, match := range urlRe.FindAllString(content, -1) {
		if !seen[match] {
			seen[match] = true
			entities = append(entities, Entity{Name: match, Type: "reference"})
		}
	}

	return entities
}

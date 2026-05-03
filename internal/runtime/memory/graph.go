package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// GraphEdge represents a typed relationship between two entities or memories.
type GraphEdge struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Relation  string    `json:"relation"` // related_to, authored_by, depends_on, decided_in, supersedes, replaced_by
	Weight    float64   `json:"weight"`
	CreatedAt time.Time `json:"created_at"`
}

// MemoryGraph provides a lightweight in-process graph for relational reasoning.
// Stored as an adjacency list in a JSON file.
type MemoryGraph struct {
	edges map[string][]GraphEdge // keyed by "from" node
	path  string
	mu    sync.RWMutex
}

// NewMemoryGraph creates a new memory graph backed by a JSON file.
func NewMemoryGraph(dir string) *MemoryGraph {
	return &MemoryGraph{
		edges: make(map[string][]GraphEdge),
		path:  filepath.Join(dir, "_graph.json"),
	}
}

// Load reads the graph from disk.
func (g *MemoryGraph) Load() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	data, err := os.ReadFile(g.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load graph: %w", err)
	}

	return json.Unmarshal(data, &g.edges)
}

// Save persists the graph to disk.
func (g *MemoryGraph) Save() error {
	g.mu.RLock()
	data, err := json.MarshalIndent(g.edges, "", "  ")
	g.mu.RUnlock()
	if err != nil {
		return err
	}

	dir := filepath.Dir(g.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(g.path, data, 0o644)
}

// AddEdge adds a directed edge to the graph.
func (g *MemoryGraph) AddEdge(from, to, relation string, weight float64) error {
	g.mu.Lock()
	// Check for duplicate.
	for _, e := range g.edges[from] {
		if e.To == to && e.Relation == relation {
			g.mu.Unlock()
			return nil // already exists
		}
	}
	g.edges[from] = append(g.edges[from], GraphEdge{
		From:      from,
		To:        to,
		Relation:  relation,
		Weight:    weight,
		CreatedAt: time.Now(),
	})
	g.mu.Unlock()
	return g.Save()
}

// FindRelated returns nodes connected to the given node by a single hop.
// If relation is empty, all relation types are returned.
func (g *MemoryGraph) FindRelated(name, relation string) []GraphEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var results []GraphEdge

	// Outgoing edges.
	for _, e := range g.edges[name] {
		if relation == "" || e.Relation == relation {
			results = append(results, e)
		}
	}

	// Incoming edges (reverse lookup).
	for from, edges := range g.edges {
		if from == name {
			continue
		}
		for _, e := range edges {
			if e.To == name && (relation == "" || e.Relation == relation) {
				results = append(results, e)
			}
		}
	}

	return results
}

// Traverse performs BFS from start up to maxDepth hops.
// Returns all edges encountered. Bounded to prevent runaway traversal.
func (g *MemoryGraph) Traverse(start string, maxDepth int) []GraphEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if maxDepth <= 0 {
		maxDepth = 2
	}

	visited := map[string]bool{start: true}
	queue := []string{start}
	var allEdges []GraphEdge
	maxEdges := 50 // hard limit

	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var nextQueue []string
		for _, node := range queue {
			for _, e := range g.edges[node] {
				allEdges = append(allEdges, e)
				if len(allEdges) >= maxEdges {
					return allEdges
				}
				if !visited[e.To] {
					visited[e.To] = true
					nextQueue = append(nextQueue, e.To)
				}
			}
		}
		queue = nextQueue
	}

	return allEdges
}

// RemoveEdges removes all edges involving the given node (both directions).
func (g *MemoryGraph) RemoveEdges(name string) error {
	g.mu.Lock()
	delete(g.edges, name)

	for from, edges := range g.edges {
		var kept []GraphEdge
		for _, e := range edges {
			if e.To != name {
				kept = append(kept, e)
			}
		}
		if len(kept) != len(edges) {
			g.edges[from] = kept
		}
	}
	g.mu.Unlock()
	return g.Save()
}

// EdgeCount returns the total number of edges.
func (g *MemoryGraph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	count := 0
	for _, edges := range g.edges {
		count += len(edges)
	}
	return count
}

// BuildEntityCooccurrenceEdges creates "related_to" edges between memories
// that share entities. This is called during dreamer consolidation.
func (g *MemoryGraph) BuildEntityCooccurrenceEdges(store *PersistentEntityStore) int {
	added := 0
	entities := store.AllEntities()

	for _, entity := range entities {
		refs := entity.MemoryRefs
		// Create edges between all pairs of memories sharing this entity.
		for i := 0; i < len(refs); i++ {
			for j := i + 1; j < len(refs); j++ {
				weight := EntityBoostAttenuation(len(refs))
				g.mu.Lock()
				// Check if edge already exists.
				exists := false
				for _, e := range g.edges[refs[i]] {
					if e.To == refs[j] && e.Relation == "related_to" {
						exists = true
						break
					}
				}
				if !exists {
					g.edges[refs[i]] = append(g.edges[refs[i]], GraphEdge{
						From:      refs[i],
						To:        refs[j],
						Relation:  "related_to",
						Weight:    weight,
						CreatedAt: time.Now(),
					})
					added++
				}
				g.mu.Unlock()
			}
		}
	}

	if added > 0 {
		_ = g.Save()
	}
	return added
}

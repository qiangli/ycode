package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/ycode/pkg/memex/graph"
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
// Stored as an adjacency list in a JSON file. Optionally mirrors writes
// into a *graph.Graph (bonsai) so callers can issue DQL queries against
// the same edges via Query.
type MemoryGraph struct {
	edges map[string][]GraphEdge // keyed by "from" node
	path  string
	mu    sync.RWMutex

	// Optional bonsai twin. When set, AddEdge / BuildEntityCooccurrenceEdges
	// dual-write to it. Wired by the memex umbrella; never required.
	twin *graph.Graph
}

// NewMemoryGraph creates a new memory graph backed by a JSON file.
func NewMemoryGraph(dir string) *MemoryGraph {
	return &MemoryGraph{
		edges: make(map[string][]GraphEdge),
		path:  filepath.Join(dir, "_graph.json"),
	}
}

// WithGraph wires an optional bonsai twin so subsequent edge writes are
// also persisted as N-Quads in the queryable graph store. Returns the
// receiver for chaining. Pass nil to detach.
func (g *MemoryGraph) WithGraph(twin *graph.Graph) *MemoryGraph {
	g.mu.Lock()
	g.twin = twin
	g.mu.Unlock()
	return g
}

// Query runs a DQL query against the bonsai twin. Returns an error if no
// twin is wired. Vars may be nil.
func (g *MemoryGraph) Query(ctx context.Context, dql string, vars map[string]string) ([]byte, error) {
	g.mu.RLock()
	twin := g.twin
	g.mu.RUnlock()
	if twin == nil {
		return nil, fmt.Errorf("memory.MemoryGraph: no graph twin wired; call WithGraph first")
	}
	return twin.Query(ctx, dql, vars)
}

// sinkEdge dual-writes a single edge into the bonsai twin if set. Errors
// are logged best-effort and never returned: the JSON file remains the
// source of truth, the twin is for query convenience.
func (g *MemoryGraph) sinkEdge(e GraphEdge) {
	if g.twin == nil {
		return
	}
	nquads := edgeToNQuads(e)
	if len(nquads) == 0 {
		return
	}
	if _, err := g.twin.Mutate(context.Background(), nquads); err != nil {
		slog.Debug("memory.MemoryGraph: twin sink failed", "err", err, "from", e.From, "to", e.To)
	}
}

// edgeToNQuads renders one memory.GraphEdge as N-Quads RDF text.
//
// Schema mapping (see pkg/memex/graph/schema.go):
//   - Memory nodes are addressed by memory.name (which has @upsert).
//   - Outgoing edges become memory.related_to / memory.supersedes /
//     memory.derived_from depending on the Relation field.
//   - Other relations (authored_by, depends_on, decided_in, replaced_by)
//     are mapped to memory.related_to with a facet `relation=<original>`
//     so the original semantic is recoverable on read.
func edgeToNQuads(e GraphEdge) []byte {
	if e.From == "" || e.To == "" {
		return nil
	}
	from := nquadEscape(e.From)
	to := nquadEscape(e.To)
	pred := relationToPredicate(e.Relation)
	var b strings.Builder
	// Upsert nodes by name (the schema marks memory.name as @upsert, so
	// repeated writes converge on the same UID).
	fmt.Fprintf(&b, "_:from <memory.name> %q .\n", from)
	fmt.Fprintf(&b, "_:from <dgraph.type> \"Memory\" .\n")
	fmt.Fprintf(&b, "_:to <memory.name> %q .\n", to)
	fmt.Fprintf(&b, "_:to <dgraph.type> \"Memory\" .\n")
	if pred == "memory.related_to" && e.Relation != "related_to" && e.Relation != "" {
		fmt.Fprintf(&b, "_:from <%s> _:to (relation=%q, weight=%g) .\n", pred, e.Relation, e.Weight)
	} else {
		fmt.Fprintf(&b, "_:from <%s> _:to .\n", pred)
	}
	return []byte(b.String())
}

func relationToPredicate(rel string) string {
	switch rel {
	case "supersedes":
		return "memory.supersedes"
	case "derived_from":
		return "memory.derived_from"
	default:
		return "memory.related_to"
	}
}

// nquadEscape escapes characters that would break N-Quad string literals.
func nquadEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return r.Replace(s)
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
	edge := GraphEdge{
		From:      from,
		To:        to,
		Relation:  relation,
		Weight:    weight,
		CreatedAt: time.Now(),
	}
	g.edges[from] = append(g.edges[from], edge)
	g.mu.Unlock()
	g.sinkEdge(edge)
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
				var newEdge GraphEdge
				inserted := false
				if !exists {
					newEdge = GraphEdge{
						From:      refs[i],
						To:        refs[j],
						Relation:  "related_to",
						Weight:    weight,
						CreatedAt: time.Now(),
					}
					g.edges[refs[i]] = append(g.edges[refs[i]], newEdge)
					added++
					inserted = true
				}
				g.mu.Unlock()
				if inserted {
					g.sinkEdge(newEdge)
				}
			}
		}
	}

	if added > 0 {
		_ = g.Save()
	}
	return added
}

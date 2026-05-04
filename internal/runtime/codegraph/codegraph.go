// Package codegraph wraps gfy's knowledge graph pipeline for ycode.
// It builds, caches, and queries code structure graphs that are used
// by /init for AGENTS.md generation and by graph query tools during
// coding sessions.
package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/qiangli/gfy/pkg/analyze"
	"github.com/qiangli/gfy/pkg/build"
	"github.com/qiangli/gfy/pkg/cluster"
	"github.com/qiangli/gfy/pkg/detect"
	"github.com/qiangli/gfy/pkg/extract"
	"github.com/qiangli/gfy/pkg/graph"
	"github.com/qiangli/gfy/pkg/search"
	"github.com/qiangli/gfy/pkg/types"
)

// DefaultCachePath is the relative path under project root where the graph is cached.
const DefaultCachePath = ".agents/ycode/graph.json"

// GraphContext holds the built graph and derived analysis results.
type GraphContext struct {
	Graph       *graph.Graph
	Communities map[int][]string
	GodNodes    []analyze.GodNode
	Surprises   []analyze.SurprisingConnection
	Stats       Stats
}

// Stats holds basic graph statistics.
type Stats struct {
	NodeCount      int
	EdgeCount      int
	CommunityCount int
	Languages      []string
	FilesAnalyzed  int
}

// Build runs the full gfy pipeline: detect → extract → build → cluster → analyze.
// Returns the graph context and caches the result to the given path.
func Build(cwd string) (*GraphContext, error) {
	// Phase 1: Detect files.
	detection := detect.Detect(cwd, false)
	if detection == nil {
		return nil, fmt.Errorf("file detection returned no results")
	}

	codeFiles := detection.Files[types.Code]
	if len(codeFiles) == 0 {
		return nil, fmt.Errorf("no code files found in %s", cwd)
	}

	// Phase 2: Extract AST symbols.
	extraction := extract.Extract(codeFiles, cwd)
	if extraction == nil {
		return nil, fmt.Errorf("extraction returned no results")
	}

	// Phase 3: Build graph.
	g := build.BuildFromResult(extraction, false)
	if g == nil {
		return nil, fmt.Errorf("graph construction failed")
	}

	// Phase 4: Detect communities.
	communities := cluster.Cluster(g)

	// Phase 5: Analyze.
	godNodes := analyze.GodNodes(g, 15)
	surprises := analyze.SurprisingConnections(g, communities, 10)

	// Detect languages from file extensions.
	languages := detectLanguages(codeFiles)

	ctx := &GraphContext{
		Graph:       g,
		Communities: communities,
		GodNodes:    godNodes,
		Surprises:   surprises,
		Stats: Stats{
			NodeCount:      g.NodeCount(),
			EdgeCount:      g.EdgeCount(),
			CommunityCount: len(communities),
			Languages:      languages,
			FilesAnalyzed:  len(codeFiles),
		},
	}

	return ctx, nil
}

// Save caches the graph to the given path (typically .agents/ycode/graph.json).
func (gc *GraphContext) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	return gc.Graph.SaveJSON(path)
}

// Load reads a cached graph and rebuilds the analysis.
// Returns nil, nil if the cache file doesn't exist.
func Load(path string) (*GraphContext, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	g, err := graph.LoadJSON(path)
	if err != nil {
		return nil, fmt.Errorf("load cached graph: %w", err)
	}

	communities := cluster.Cluster(g)
	godNodes := analyze.GodNodes(g, 15)
	surprises := analyze.SurprisingConnections(g, communities, 10)

	return &GraphContext{
		Graph:       g,
		Communities: communities,
		GodNodes:    godNodes,
		Surprises:   surprises,
		Stats: Stats{
			NodeCount:      g.NodeCount(),
			EdgeCount:      g.EdgeCount(),
			CommunityCount: len(communities),
		},
	}, nil
}

// CachePath returns the full cache path for a project root.
func CachePath(cwd string) string {
	return filepath.Join(cwd, DefaultCachePath)
}

// Summary renders an LLM-friendly text summary of the code graph.
// Designed to be included in the /init prompt context.
func (gc *GraphContext) Summary() string {
	var b strings.Builder

	// Stats overview.
	fmt.Fprintf(&b, "### Graph Overview\n")
	fmt.Fprintf(&b, "- **Entities:** %d nodes, %d edges\n", gc.Stats.NodeCount, gc.Stats.EdgeCount)
	fmt.Fprintf(&b, "- **Communities:** %d logical module groups\n", gc.Stats.CommunityCount)
	fmt.Fprintf(&b, "- **Files analyzed:** %d\n", gc.Stats.FilesAnalyzed)
	if len(gc.Stats.Languages) > 0 {
		fmt.Fprintf(&b, "- **Languages detected:** %s\n", strings.Join(gc.Stats.Languages, ", "))
	}
	b.WriteString("\n")

	// God nodes — architectural linchpins.
	if len(gc.GodNodes) > 0 {
		b.WriteString("### Architectural Linchpins (most connected entities)\n")
		b.WriteString("These are the most interconnected code entities — changes here have the widest impact.\n\n")
		b.WriteString("| Entity | Connections |\n")
		b.WriteString("|--------|-------------|\n")
		for _, gn := range gc.GodNodes {
			fmt.Fprintf(&b, "| %s | %d |\n", gn.Label, gn.Degree)
		}
		b.WriteString("\n")
	}

	// Communities — logical module boundaries.
	if len(gc.Communities) > 0 {
		b.WriteString("### Module Communities\n")
		b.WriteString("Code clusters detected by graph analysis — these suggest logical module boundaries.\n\n")

		// Sort by community ID for determinism.
		ids := make([]int, 0, len(gc.Communities))
		for cid := range gc.Communities {
			ids = append(ids, cid)
		}
		sort.Ints(ids)

		for _, cid := range ids {
			members := gc.Communities[cid]
			if len(members) == 0 {
				continue
			}
			// Show representative members (up to 8).
			shown := members
			if len(shown) > 8 {
				shown = shown[:8]
			}
			labels := make([]string, len(shown))
			for i, id := range shown {
				attrs := gc.Graph.NodeAttrs(id)
				if label, ok := attrs["label"].(string); ok {
					labels[i] = label
				} else {
					labels[i] = id
				}
			}
			suffix := ""
			if len(members) > 8 {
				suffix = fmt.Sprintf(" +%d more", len(members)-8)
			}
			fmt.Fprintf(&b, "- **Community %d** (%d members): %s%s\n",
				cid, len(members), strings.Join(labels, ", "), suffix)
		}
		b.WriteString("\n")
	}

	// Surprising connections — unexpected coupling.
	if len(gc.Surprises) > 0 {
		b.WriteString("### Cross-Module Connections\n")
		b.WriteString("Unexpected relationships between separate code areas — potential coupling or integration points.\n\n")
		for _, s := range gc.Surprises {
			files := ""
			if len(s.SourceFiles) > 0 {
				files = " (" + strings.Join(s.SourceFiles, " ↔ ") + ")"
			}
			fmt.Fprintf(&b, "- **%s** → **%s** [%s]%s — %s\n",
				s.Source, s.Target, s.Relation, files, s.Why)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// QueryGraph searches for nodes by keyword and returns a BFS subgraph.
func (gc *GraphContext) QueryGraph(question string, depth int) string {
	if depth <= 0 {
		depth = 2
	}
	results := search.ScoreNodes(gc.Graph, question)
	if len(results) > 5 {
		results = results[:5]
	}
	if len(results) == 0 {
		return "No matching nodes found for: " + question
	}
	startNodes := make([]string, len(results))
	for i, r := range results {
		startNodes[i] = r.ID
	}
	visited, edges := gc.Graph.BFS(startNodes, depth)
	return subgraphToText(gc.Graph, visited, edges)
}

// GetNode looks up a node by label or ID.
func (gc *GraphContext) GetNode(label string) string {
	id := search.FindNode(gc.Graph, label)
	if id == "" {
		return "Node not found: " + label
	}
	attrs := gc.Graph.NodeAttrs(id)
	return fmt.Sprintf("ID: %s\nLabel: %s\nType: %s\nSource: %s\nDegree: %d",
		id, attrStr(attrs, "label"), attrStr(attrs, "file_type"),
		attrStr(attrs, "source_file"), gc.Graph.Degree(id))
}

// GetNeighbors returns direct neighbors of a node with edge metadata.
func (gc *GraphContext) GetNeighbors(label, relationFilter string) string {
	id := search.FindNode(gc.Graph, label)
	if id == "" {
		return "Node not found: " + label
	}
	var lines []string
	for _, nb := range gc.Graph.Neighbors(id) {
		eAttrs := gc.Graph.EdgeAttrs(id, nb)
		rel := attrStr(eAttrs, "relation")
		if relationFilter != "" && rel != relationFilter {
			continue
		}
		conf := attrStr(eAttrs, "confidence")
		nbAttrs := gc.Graph.NodeAttrs(nb)
		lines = append(lines, fmt.Sprintf("- %s (%s) [%s, %s]",
			attrStr(nbAttrs, "label"), attrStr(nbAttrs, "file_type"), rel, conf))
	}
	if len(lines) == 0 {
		return "No neighbors found"
	}
	return strings.Join(lines, "\n")
}

// GetCommunity returns all nodes in a community.
func (gc *GraphContext) GetCommunity(communityID int) string {
	nodes, ok := gc.Communities[communityID]
	if !ok {
		return fmt.Sprintf("Community %d not found", communityID)
	}
	var lines []string
	for _, nid := range nodes {
		attrs := gc.Graph.NodeAttrs(nid)
		lines = append(lines, fmt.Sprintf("- %s (%s) [degree %d]",
			attrStr(attrs, "label"), attrStr(attrs, "file_type"), gc.Graph.Degree(nid)))
	}
	return fmt.Sprintf("Community %d (%d nodes):\n%s",
		communityID, len(nodes), strings.Join(lines, "\n"))
}

// GetGodNodes returns the most connected entities.
func (gc *GraphContext) GetGodNodes(topN int) string {
	if topN <= 0 {
		topN = 10
	}
	gods := gc.GodNodes
	if len(gods) > topN {
		gods = gods[:topN]
	}
	var lines []string
	for i, gn := range gods {
		lines = append(lines, fmt.Sprintf("%d. %s (degree %d)", i+1, gn.Label, gn.Degree))
	}
	return strings.Join(lines, "\n")
}

// GetGraphStats returns node/edge counts and community info.
func (gc *GraphContext) GetGraphStats() string {
	confCounts := map[string]int{}
	for _, e := range gc.Graph.Edges() {
		conf := attrStr(e.Attrs, "confidence")
		confCounts[conf]++
	}
	return fmt.Sprintf("Nodes: %d\nEdges: %d\nCommunities: %d\nExtracted: %d\nInferred: %d\nAmbiguous: %d\nFiles: %d\nLanguages: %s",
		gc.Stats.NodeCount, gc.Stats.EdgeCount, gc.Stats.CommunityCount,
		confCounts["EXTRACTED"], confCounts["INFERRED"], confCounts["AMBIGUOUS"],
		gc.Stats.FilesAnalyzed, strings.Join(gc.Stats.Languages, ", "))
}

// ShortestPath finds the shortest path between two nodes.
func (gc *GraphContext) ShortestPath(source, target string, maxHops int) string {
	srcID := search.FindNode(gc.Graph, source)
	tgtID := search.FindNode(gc.Graph, target)
	if srcID == "" {
		return "Source not found: " + source
	}
	if tgtID == "" {
		return "Target not found: " + target
	}
	path := gc.Graph.ShortestPath(srcID, tgtID, maxHops)
	if path == nil {
		return "No path found"
	}
	var labels []string
	for _, nid := range path {
		attrs := gc.Graph.NodeAttrs(nid)
		labels = append(labels, attrStr(attrs, "label"))
	}
	return fmt.Sprintf("Path (%d hops): %s", len(path)-1, strings.Join(labels, " → "))
}

// detectLanguages extracts unique languages from file extensions.
func detectLanguages(files []string) []string {
	langMap := map[string]bool{}
	extToLang := map[string]string{
		".go": "Go", ".py": "Python", ".js": "JavaScript", ".ts": "TypeScript",
		".jsx": "JavaScript", ".tsx": "TypeScript", ".rs": "Rust", ".java": "Java",
		".c": "C", ".cpp": "C++", ".rb": "Ruby", ".swift": "Swift",
		".kt": "Kotlin", ".cs": "C#", ".scala": "Scala", ".php": "PHP",
		".lua": "Lua", ".zig": "Zig", ".ex": "Elixir", ".jl": "Julia",
		".m": "Objective-C", ".dart": "Dart", ".v": "Verilog",
		".vue": "Vue", ".svelte": "Svelte",
	}
	for _, f := range files {
		ext := filepath.Ext(f)
		if lang, ok := extToLang[ext]; ok {
			langMap[lang] = true
		}
	}
	langs := make([]string, 0, len(langMap))
	for lang := range langMap {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}

func attrStr(attrs map[string]any, key string) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func subgraphToText(g *graph.Graph, nodeIDs []string, edges []graph.EdgeData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Subgraph: %d nodes, %d edges\n\n", len(nodeIDs), len(edges))
	b.WriteString("Nodes:\n")
	for _, id := range nodeIDs {
		attrs := g.NodeAttrs(id)
		fmt.Fprintf(&b, "  - %s (%s)\n", attrStr(attrs, "label"), attrStr(attrs, "file_type"))
	}
	if len(edges) > 0 {
		b.WriteString("\nEdges:\n")
		for _, e := range edges {
			srcLabel := attrStr(g.NodeAttrs(e.Source), "label")
			tgtLabel := attrStr(g.NodeAttrs(e.Target), "label")
			rel := attrStr(e.Attrs, "relation")
			fmt.Fprintf(&b, "  - %s -[%s]-> %s\n", srcLabel, rel, tgtLabel)
		}
	}
	return b.String()
}

// codeExtensions maps file extensions that should trigger graph invalidation.
var codeExtensions = map[string]bool{
	".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".rs": true, ".java": true, ".c": true, ".cpp": true, ".rb": true, ".swift": true,
	".kt": true, ".cs": true, ".scala": true, ".php": true, ".lua": true, ".zig": true,
	".ex": true, ".jl": true, ".m": true, ".dart": true, ".v": true,
	".vue": true, ".svelte": true,
}

// Manager provides thread-safe, session-wide access to the code knowledge graph.
// It tracks file changes and rebuilds the graph asynchronously when stale.
type Manager struct {
	mu         sync.RWMutex
	gc         *GraphContext
	cwd        string
	cachePath  string
	dirty      atomic.Bool
	rebuilding atomic.Bool
}

// NewManager creates a graph manager for the given project root.
// It attempts to load a cached graph from .agents/ycode/graph.json.
func NewManager(cwd string) *Manager {
	m := &Manager{
		cwd:       cwd,
		cachePath: CachePath(cwd),
	}
	// Try loading cached graph.
	gc, err := Load(m.cachePath)
	if err != nil {
		slog.Debug("codegraph: failed to load cached graph", "error", err)
	}
	if gc != nil {
		m.gc = gc
		slog.Info("codegraph: loaded cached graph", "nodes", gc.Stats.NodeCount, "edges", gc.Stats.EdgeCount)
	}
	return m
}

// Get returns the current graph context under a read lock.
// Returns nil if no graph has been built yet.
func (m *Manager) Get() *GraphContext {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gc
}

// Set atomically replaces the graph and saves the cache.
func (m *Manager) Set(gc *GraphContext) {
	m.mu.Lock()
	m.gc = gc
	m.dirty.Store(false)
	m.mu.Unlock()

	if gc != nil {
		if err := gc.Save(m.cachePath); err != nil {
			slog.Warn("codegraph: failed to save cache", "error", err)
		}
	}
}

// NotifyFileChanged marks the graph as dirty if the path is a code file.
// Called asynchronously by the file write hook — must be non-blocking.
func (m *Manager) NotifyFileChanged(path string) {
	ext := filepath.Ext(path)
	if codeExtensions[ext] {
		m.dirty.Store(true)
	}
}

// RebuildIfDirty triggers an asynchronous rebuild if the graph is stale.
// Non-blocking — returns immediately. The rebuild runs in a background goroutine.
func (m *Manager) RebuildIfDirty(ctx context.Context) {
	if !m.dirty.Load() {
		return
	}
	// Prevent concurrent rebuilds.
	if !m.rebuilding.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer m.rebuilding.Store(false)
		if err := m.Rebuild(ctx); err != nil {
			slog.Warn("codegraph: background rebuild failed", "error", err)
		}
	}()
}

// Rebuild runs a synchronous full rebuild of the code knowledge graph.
// The graph is atomically swapped and the cache is updated.
func (m *Manager) Rebuild(ctx context.Context) error {
	slog.Info("codegraph: rebuilding graph", "cwd", m.cwd)

	gc, err := Build(m.cwd)
	if err != nil {
		return fmt.Errorf("rebuild graph: %w", err)
	}

	m.Set(gc)
	slog.Info("codegraph: rebuild complete", "nodes", gc.Stats.NodeCount, "edges", gc.Stats.EdgeCount)
	return nil
}

// IsDirty returns whether the graph needs rebuilding.
func (m *Manager) IsDirty() bool {
	return m.dirty.Load()
}
